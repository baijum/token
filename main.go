package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/urfave/negroni"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

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
		sa := &corev1.ServiceAccount{}
		sa.GenerateName = "chart-verifier-ci-" + id + "-"
		sa, err := clientset.CoreV1().ServiceAccounts("chart-verifier-infra").Create(ctx, sa, metav1.CreateOptions{})
		if err != nil {
			log.Printf("[ERROR] Error creating service account: %v", err.Error())
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sa.Name))
	}
}
func main() {
	r := mux.NewRouter()

	r.HandleFunc("/api/token/{id}", TokenHandler).Methods("GET", "DELETE")

	n := negroni.Classic()
	n.UseHandler(r)
	n.Run(":7080")
}
