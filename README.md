# Mutating Admission Controller - How to create and use a Webhook
## Prerequisites
Availability of
Kubernetes implementation such as kind, minikube, Docker Desktop.
Docker account.
Linux, macOS or Windows.
Sufficient memory to deploy Docker and Kubernetes or a VM.
The webhook assumes a local installation. For production environments a cert-manager will be used for issueing and maintaining TLS certificates.

## Security related activities

### Using cfssl and cfssljson
The webhook requires  a certificate to cooperate with Kubernetes (API Server). 
In the following sections a self-signed certificate is created. For production environment  a certificate with a real and accepted CA is necessary. In particular, the use of a Cert-manager is helpful.

Create a docker development container:

	docker run -it --rm -v ${PWD}:/work -w /work debian bash

**Note:**
On Windows the equivalent to ${HOME} is %USERPROFILE%.
When using Powershell (Get-Location).Path retrieves the current directory. There ypu may also use in PowerShell:
$ENV:HOME = $env:USERPROFILE
and
$env:PWD = (Get.Location).Path

#### Install certificate with cfssl:

Install cfssl tools cfssl and cfssljson

	apt-get update && apt-get install -y curl &&
	curl -L https://github.com/cloudflare/cfssl/releases/download/v1.5.0/cfssl_1.5.0_linux_amd64 -o /usr/local/bin/cfssl && \
	curl -L https://github.com/cloudflare/cfssl/releases/download/v1.5.0/cfssljson_1.5.0_linux_amd64 -o /usr/local/bin/cfssljson && \
	chmod +x /usr/local/bin/cfssl && \
	chmod +x /usr/local/bin/cfssljson

#### Generate ca in /tmp
	cfssl gencert -initca ./tls/ca-csr.json | cfssljson -bare /tmp/ca
See also files ca\_config.json and ca\_csr.json

#### Generate certificate in /tmp
	cfssl gencert \
	  -ca=/tmp/ca.pem \
	  -ca-key=/tmp/ca-key.pem \
	  -config=./tls/ca-config.json \
	  -hostname="mutating-webhook,mutating-webhook.default.svc.cluster.local,mutating-webhook.default.svc,localhost,127.0.0.1" \
	  -profile=default \
	  ./tls/ca-csr.json | cfssljson -bare /tmp/mutating-webhook
#### Creating a secret with the generated TLS certificate

	cat <<EOF > ./tls/mutating-webhook-tls.yaml
	apiVersion: v1
	kind: Secret
	metadata:
	  name: mutating-webhook-tls
	type: Opaque
	data:
	  tls.crt: $(cat /tmp/mutating-webhook.pem | base64 | tr -d '\n')
	  tls.key: $(cat /tmp/mutating-webhook-key.pem | base64 | tr -d '\n') 
	EOF

#### Creating CA\_BUNDLE and injecting it to webhook.yaml
Create CA Bundle and add it to the template
	ca_pem_b64="$(openssl base64 -A <"/tmp/ca.pem")"
	
	sed -e 's@${CA_PEM_B64}@'"$ca_pem_b64"'@g' <"webhook-template.yaml"  > webhook.yaml


Look into local tls-directory and  Exit Container!

#### Webhook Provisioning
If you want to see what our webhook is going to deliver, look into the webhook.yaml file. It offers a mutating webhook (kind: MutatingWebhookConfiguration). The version of ingoing Admission Reviews is set. In addition it defines how pods can specify to be handled by the mutating web hook. In particular, objects/pods need to define the label: mutating-webhook-enabled: true. We might also use namespaces instead, so that pods within a specific namespace are selected for mutation. In ClientConfig it is specified how Kubernetes can call the webhook. It is handled by a microservice call with the path /mutate. The rules section tells Kubernetes to call the webhook only for pods and only for CREATE-events.

### Determining the go version used
Run 
	go version

## Test of integration
cd to the webhook/src-directory.

Copy dockerfile\_test to dockerfile
	FROM golang:1.22-alpine as dev-env
	
	WORKDIR /app

Run the following command to create a Go-development-container:
	docker build . -t webhook
	docker run -it --rm -p 80:80 -v ${PWD}:/app webhook sh

The webhook will serve requests on port 80.

Note: If you get the error that the env variables KUBERNETES\_SERVICE\_HOST resp. KUBERNETES\_SERVICE\_PORT are not set, look to .kube/config in your home directory. Set and export the two env variables above properly.
For example:
	export KUBERNETES_SERVICE_HOST=https://127.0.0.1
	export KUBERNETES_SERVICE_HOST=6443
Run the docker command again


Initialize Go module calling 
	go mod init mutating-webhook

Write initial Go program for testing (otherwise proceed with section Mutating Webhook). 
	package main
	
	import (
	  "net/http"
		"log"
	)
	
	func main() {
	  http.HandleFunc("/", HandleRoot)
		http.HandleFunc("/mutate", HandleMutate)
	  log.Fatal(http.ListenAndServe(":80", nil))
	}
	
	func HandleRoot(w http.ResponseWriter, r *http.Request){
		w.Write([]byte(„handling Root!"))
	}
	
	func HandleMutate(w http.ResponseWriter, r *http.Request){
		w.Write([]byte(„handling mutate“))
	}

Execute:
	 go run main.go

Open a browser outside of the container and enter the URL localhost resp. localhost/mutate. Exit container!

## Mutating Webhook
Now the real mutation webhook is created:

On Windows (unlike Linux, macOS) the cooperation of the container with the webhook and the container with kubernetes (using kind or Docker Desktop with Kubernetes enabled) is not possible per default.
Thus, we need the exit the app container and provide a new one using —net host:
Change to webhook/src-directory.
Copy dockerfile\_final  to become dockerfile. Start the final development container for the webhook.

	docker run -it --rm --net host -v ${HOME}/.kube/:/root/.kube/ -v ${PWD}:/app webhook sh

See above, when getting errors. The env variables KUBERNETES\_SERVICE\_HOST and KUBERNETES\_SERVICE\_PORT must be set correctly.

In the container execute:
	apk add --no-cache curl
	curl -LO https://storage.googleapis.com/kubernetes-release/release/`curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt`/bin/linux/amd64/kubectl
	chmod +x ./kubectl
	mv ./kubectl /usr/local/bin/kubectl
Now run:
	kubect get nodes
… which should show one node.

Get the Kubernetes Runtime Serializer that is responsible for handling events that Kubernetes sends to the webhook. In addition a new UniversalDeserializer is created as a global variable. The serializer translates K8s events to structs.

To authenticate with Kubernetes the webhook might either a service account or a  Token (production) or .kube/config (development). The code can handle both situations. See variables var config *rest.config and var clients *kubernetes.Clientset.

When developing locally, useKubeConfig and kubeConfigFilePath (which config file to use). When not specified, .kube/config iin the home directory s used. In this case the webhook reads the respective environment variables from the OS. 
An if-statement is used to decide which of the two mechanisms (config vs. token) is going to be used for authentication.

We need to obtain the client-go library. For this purpse, we need to specify the exact version we need.
This could be:
	k8s.io/client-go@latest
or a specific version:
	k8s.io/client-go@v0.21.0



Read in 
	https://github.com/kubernetes/client-go/blob/master/README.md 
what version of client go to use.


Note: it is necessary to get the right version of Go Client when the Kubernetes version changes (e.g. when upgrading).

Install dependencies:
Get all dependencies (i.e. Go libraries)
	go get k8s.io/client-go@kubernetes-1.29.2
	go get k8s.io/api@kubernetes-1.29.2
	go get k8s.io/apimachinery@kubernetes-1.29.2
	go get k8s.io/utils
	go get k8s.io/api/admission/v1beta1 
	go get k8s.io/api/core/v1
	go get k8s.io/apimachinery/pkg/apis/meta/v1
	go get k8s.io/apimachinery/pkg/runtime
	go get k8s.io/apimachinery/pkg/runtime/serializer
	go get k8s.io/client-go/kubernetes
	go get k8s.io/client-go/rest
	go get k8s.io/client-go/tools/clientcmd
	go get k8s.io/client-go/util/homedir


Build webhook:
	go build -o webhook

Define a tag for the image. Replace all <your-tag>-appearances below with an actual tag. Do not forget to change <your-tag> in deployment.yaml accordingly.

Now use existing dockerfile\_final in webhook/src after exiting the development container.
	cp dockerfile_final dockerfile

In the webhook’s src-directory use the command:
	docker build --platform linux/amd64 . -t <your tag>/mutating-webhook:v1
Push the webhook image to the docker registry and pull it later when needed.

	docker push <your-tag>/mutating-webhook:v1
Apply the yaml-files:
Note: mutating-webhook-tls.yaml was created in the webhook’s tls subdirectory, all other .yaml-files can be found in the webhook directory.
In webhook-directory apply the following yaml-files.

	kubectl apply -f  tls/mutating-webhook-tls.yaml
	kubectl apply -f rbac.yaml
	kubectl apply -f deployment.yaml
	kubectl apply -f webhook.yaml
Now the webhook is running. To test it use a demo pod:
	kubectl apply -f demo-pod.yaml
This yaml-file is very simple. It just uses nginx and defines the necessary label  mutating-webhook-enabled.
	apiVersion: v1
	kind: Pod
	metadata:
	  name: demo-pod
	  labels:
	    mutating-webhook-enabled: "true"
	spec:
	  containers:
	  - name: nginx
	    image: nginx 

After it is started call 
	kubectl get pods --show-labels

You should see a message similar to:
	demo-pod                          1/1     Running   0          13m   mutating-webhook-enabled=true,mutating-webhook=it-worked

Note: Each pod that should be mutated must contain the label mutating-webhook-enable set to true.
After mutation it has the new label mutating-webhook set to it-worked.





