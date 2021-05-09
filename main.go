package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	"github.com/urfave/negroni"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type TokenResponse struct {
	Namespace string `json:"namespace"`
	Token     string `json:"token"`
}

func ValidateRequest(token string) bool {
	// Trigger a workflow_dispatch action in the repository using the token.
	// The status must be a success (200 OK).  Since GitHub Action's token is
	// going to be used by the "Build and Verify" job, the workflow_dispatch job
	// is not going to be actually triggered.
	// Ref. https://github.community/t/how-to-verify-that-it-is-github-token/139464/2

	repository := "openshift-helm-charts/charts"
	data, err := os.ReadFile("/bindings/repository")
	if err != nil {
		log.Printf("[ERROR] reading file (/bindings/repository): %v", err)
		log.Printf("[INFO] continue with default repository: %s", repository)
	} else {
		repository = string(data)
	}

	client := &http.Client{}
	url := "https://api.github.com/repos/" + repository + "/actions/workflows/awaiting-approval-notification.yml/dispatches"
	var jsonStr = []byte(`{"ref": "main"}`)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))
	if err != nil {
		log.Printf("[ERROR] creating request: %v", err.Error())
		return false
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Authorization", "token "+token)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[ERROR] request error: %v", err.Error())
		return false
	}

	if resp.StatusCode == 204 {
		log.Printf("[DEBUG] GITHUB_TOKEN sucessfuly verified.")
		return true
	}

	log.Printf("[INFO] request URL: %v", url)
	log.Printf("[INFO] response status: %v", resp.Status)
	log.Printf("[INFO] response headers: %v", resp.Header)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[ERROR] reading response body: %v", err.Error())
		return false
	}

	log.Printf("[INFO] response body: %v", string(body))
	return false
}

func TokenHandler(w http.ResponseWriter, r *http.Request) {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Printf("[ERROR] creating in cluster config: %v", err.Error())
		return
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Printf("[ERROR] creating clientset: %v", err.Error())
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]
	token := r.Header.Get("X-GitHub-Token")
	log.Printf("[INFO] ID: %s", id)
	if valid := ValidateRequest(token); valid != true {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("{'error': 'Unauthorized request'}\n"))
		return
	}
	if r.Method == "DELETE" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("no auth required\n"))
	} else {
		ctx := context.TODO()

		/*
			ns, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
			if err != nil {
				log.Printf("[ERROR] Error reading namespace file: %v", err.Error())
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			namespace := string(ns)
		*/

		tr := &TokenResponse{}

		ns := &corev1.Namespace{}
		ns.GenerateName = "chart-verifier-ci-" + id + "-"
		ns, err = clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		if err != nil {
			log.Printf("[ERROR] Error creating namespace: %v", err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		tr.Namespace = ns.Name

		sa := &corev1.ServiceAccount{}
		sa.Name = ns.Name
		sa, err = clientset.CoreV1().ServiceAccounts(ns.Name).Create(ctx, sa, metav1.CreateOptions{})
		if err != nil {
			log.Printf("[ERROR] Error creating service account: %v", err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		role := &rbacv1.Role{}
		role.Name = "chart-verifier-ci-" + id
		role, err = clientset.RbacV1().Roles(ns.Name).Create(ctx, role, metav1.CreateOptions{})
		if err != nil {
			log.Printf("[ERROR] Error creating role: %v", err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		roleBinding := &rbacv1.RoleBinding{}
		roleBinding.Name = "chart-verifier-ci-" + id
		roleBinding, err = clientset.RbacV1().RoleBindings(ns.Name).Create(ctx, roleBinding, metav1.CreateOptions{})
		if err != nil {
			log.Printf("[ERROR] Error creating role binding: %v", err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		found := false
		for i := 0; i < 6; i++ {
			sa, err = clientset.CoreV1().ServiceAccounts(ns.Name).Get(ctx, sa.Name, metav1.GetOptions{})
			if err != nil {
				log.Printf("[ERROR] Error retrieving service account: %v", err.Error())
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			if len(sa.Secrets) >= 2 {
				found = true
				break
			}
			time.Sleep(10 * time.Second)
		}
		if found == false {
			log.Printf("[ERROR] Error creating secret: %v", err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		for _, s := range sa.Secrets {
			secret, err := clientset.CoreV1().Secrets(ns.Name).Get(ctx, s.Name, metav1.GetOptions{})
			if err != nil {
				log.Printf("[ERROR] Error retrieving secret: %v", err.Error())
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if secret.Type == "kubernetes.io/service-account-token" {
				tr.Token = string(secret.Data["token"])
				out, err := json.Marshal(tr)
				if err != nil {
					log.Printf("[ERROR] Error marshalling: %v", err.Error())
					w.WriteHeader(http.StatusInternalServerError)
					return
				}

				w.WriteHeader(http.StatusOK)
				w.Write(out)
				return
			}

		}
		log.Printf("[ERROR] Secret not found: %v", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
	}
}
func main() {
	r := mux.NewRouter()

	r.HandleFunc("/api/token/{id}", TokenHandler).Methods("GET", "DELETE")

	n := negroni.Classic()
	n.UseHandler(r)
	n.Run(":7080")
}
