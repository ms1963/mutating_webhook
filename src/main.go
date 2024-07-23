// Mutating webhook for Kubernetes that addresses pod creation events.
// If pod enables webhook, a new label is added to the pod.
// written by Michael Stal in 2024.

package main

// Import all required packages from Go library and kubernetes
import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"k8s.io/api/admission/v1beta1"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	rest "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Data structures that map Kubernetes json-information to Go

// server port and security credentials
type ServerParameters struct {
	port     int    // webhook server port
	certFile string // path to the x509 certificate for https
	keyFile  string // path to the x509 private key matching `CertFile`
}

// patch = change in pod yaml information
type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

var parameters ServerParameters

// the UniversalDeserializer is responsible to deserialize requests
// from the API Server
var (
	universalDeserializer = serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer()
)

var config *rest.Config             // used for configuration in development environment
var clientSet *kubernetes.Clientset // used for configuration in production

func main() {

	// check whether user set env-variable USE_KUBECONFIG
	useKubeConfig := os.Getenv("USE_KUBECONFIG")
	// get the config file from (often located in .kube)
	kubeConfigFilePath := os.Getenv("KUBECONFIG")

	flag.IntVar(&parameters.port, "port", 8443, "Webhook server port.")
	flag.StringVar(&parameters.certFile, "tlsCertFile", "/etc/webhook/certs/tls.crt", "File containing the x509 Certificate for HTTPS.")
	flag.StringVar(&parameters.keyFile, "tlsKeyFile", "/etc/webhook/certs/tls.key", "File containing the x509 private key to --tlsCertFile.")
	flag.Parse()

	if len(useKubeConfig) == 0 { // do not use config file
		// default to service account in cluster token
		c, err := rest.InClusterConfig()
		if err != nil {
			panic(err.Error())
		}
		config = c
	} else { // load config file
		//load from a kube config
		var kubeconfig string

		// search for .kube in homedir
		if kubeConfigFilePath == "" {
			if home := homedir.HomeDir(); home != "" {
				kubeconfig = filepath.Join(home, ".kube", "config")
			}
		} else { // user specified path to config file
			kubeconfig = kubeConfigFilePath
		}
		// Print out information on config-file
		fmt.Println("kubeconfig: " + kubeconfig)

		c, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			panic(err.Error())
		}
		config = c
	}

	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	clientSet = cs

	test()
	http.HandleFunc("/", HandleRoot)
	http.HandleFunc("/mutate", HandleMutate)
	log.Fatal(http.ListenAndServeTLS(":"+strconv.Itoa(parameters.port), parameters.certFile, parameters.keyFile, nil))
}

// just for testing in browser when https://webhook_url:port
func HandleRoot(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("HandleRoot!"))
}

// mutation case: API Server delivers a scheduling request for a new
// pod to webhook
func HandleMutate(w http.ResponseWriter, r *http.Request) {

	// read body and check for errors
	body, err := ioutil.ReadAll(r.Body)
	err = ioutil.WriteFile("/tmp/request", body, 0644)
	if err != nil {
		panic(err.Error())
	}

	// retrieve Admission Review Request from API Server
	var admissionReviewReq v1beta1.AdmissionReview

	// check for errors when universalDeserializer tries to
	// deserialize the Admission Review Request
	if _, _, err := universalDeserializer.Decode(body, nil, &admissionReviewReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Errorf("could not deserialize request: %v", err)
	} else if admissionReviewReq.Request == nil {
		w.WriteHeader(http.StatusBadRequest)
		errors.New("malformed admission review: request is nil")
	}

	// print out type, event and name, e.g., pod, create,pod_name
	fmt.Printf("Type: %v \t Event: %v \t Name: %v \n",
		admissionReviewReq.Request.Kind,
		admissionReviewReq.Request.Operation,
		admissionReviewReq.Request.Name,
	)

	var pod apiv1.Pod // this structure is filled with pod information

	// unmarshal json request and get pod information
	err = json.Unmarshal(admissionReviewReq.Request.Object.Raw, &pod)

	if err != nil {
		fmt.Errorf("could not unmarshal pod on admission request: %v", err)
	}

	// since webhook is only called for pod creation if pod yaml file,
	// has enabled the webhook, we can now apply changes. In this
	// case a new label is added to the pod. Each change is a patch.
	// Multiple patches can be applied when answering the  review request
	var patches []patchOperation

	// retrieve labels of pod that is going to be created
	labels := pod.ObjectMeta.Labels
	// add new label to pod
	labels["mutating-webhook"] = "pod was mutated"

	// append patch to list of patches
	patches = append(patches, patchOperation{
		Op:    "add",              // we add
		Path:  "/metadata/labels", // in the path
		Value: labels,             // a new label
	})

	// try to marshal patch
	patchBytes, err := json.Marshal(patches)

	if err != nil {
		fmt.Errorf("could not marshal JSON patch: %v", err)
	}

	// The Admission Review Response is used to tell Kubernetes
	// whether the pod creation request should be accepted
	// using the same UID
	admissionReviewResponse := v1beta1.AdmissionReview{
		Response: &v1beta1.AdmissionResponse{
			UID:     admissionReviewReq.Request.UID,
			Allowed: true,
		},
	}

	// the patches are reported back
	admissionReviewResponse.Response.Patch = patchBytes

	// the Admission Review Response is delivered back to
	// the API server in JSON-format as a sequence of bytes
	bytes, err := json.Marshal(&admissionReviewResponse)
	if err != nil {
		fmt.Errorf("marshaling response: %v", err)
	}

	// the bytes are written
	w.Write(bytes)

}
