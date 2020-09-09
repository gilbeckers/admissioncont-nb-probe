/*
Copyright (c) 2019 StackRox Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"

	"github.com/mattbaird/jsonpatch"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	tlsDir      = `/run/secrets/tls`
	tlsCertFile = `tls.crt`
	tlsKeyFile  = `tls.key`
)

var (
	podResource = metav1.GroupVersionResource{Version: "v1", Resource: "pods"}
)

// Tries to extract the value of a certain EnvVar in the Env
// If no EnvVar with given name is found, returns empty string and false
func extractEnvValue(envVars []corev1.EnvVar, value string) (string, bool) {
	log.Print("Looping over the env")
	for _, v := range envVars {
		log.Printf("envvar: %v -%v-", v.Name, v.Value)
		if v.Name == value {
			return v.Value, true
		}
	}
	return "", false
}

// addReadinessProbe adds the correct Readiness Probe to the notebook container in a notebook server pod.
// Therefor incoming pods are filtered on two levels:
// 1. The pod needs to have a label with key "notebook-name"
// 2. The right container is selected by looking for an envvar with name "NB_PREFIX"
// This function takes a pod, applies the needed changes and returns the changed pod.
func addReadinessProbeToNotebook(pod corev1.Pod) corev1.Pod {
	// 1. First check if correct label is present
	if podLabels := pod.GetLabels(); podLabels != nil {
		if _, ok := podLabels["notebook-name"]; ok { // check if the label notebook-name is present
			log.Print("A 'notebook-name' label found, so adding readiness probe")
			log.Printf("Notebook name: %v", podLabels["notebook-name"])
			// 2. Add the readiness probe
			// Loop over all containers in pod and try to find the correct one
			for i, ctr := range pod.Spec.Containers {
				log.Printf("The env: %v", ctr.Env)

				// Look in the Env if there is a value with name NB_PREFIX
				// In case there is, we have found the right container
				// -> use the value of NB_PREFIX envvar for the path of the readiness probe.
				if envValue, envValueFound := extractEnvValue(ctr.Env, "NB_PREFIX"); envValueFound {
					log.Printf("The current contains the env var %v with value: %v", "NB_PREFIX", envValue)

					/*
						if pod.Spec.Containers[i].ReadinessProbe == nil {
							log.Print("The current container has no Readiness Probe")
							pod.Spec.Containers[i].ReadinessProbe = new(corev1.Probe)
						}
						pod.Spec.Containers[i].ReadinessProbe.HTTPGet = new(corev1.HTTPGetAction)
						pod.Spec.Containers[i].ReadinessProbe.HTTPGet.Path = envValue + "/tree?"  // + "/"
						pod.Spec.Containers[i].ReadinessProbe.HTTPGet.Port = intstr.FromInt(8888) //intstr.FromInt(8888)
						pod.Spec.Containers[i].ReadinessProbe.InitialDelaySeconds = 15
						pod.Spec.Containers[i].ReadinessProbe.FailureThreshold = 20
						pod.Spec.Containers[i].ReadinessProbe.SuccessThreshold = 1
						log.Printf("The new created readiness probe: %v", pod.Spec.Containers[i].ReadinessProbe)
					*/
					pod.Spec.Containers[i].ReadinessProbe = &corev1.Probe{
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Path:   envValue + "/tree?",
								Port:   intstr.FromInt(int(pod.Spec.Containers[i].Ports[0].ContainerPort)), //intstr.FromInt(8888),
								Scheme: corev1.URISchemeHTTP,
							},
						},
						InitialDelaySeconds: 15,
						SuccessThreshold:    1,
						FailureThreshold:    5,
						//TimeoutSeconds: 10,
						//PeriodSeconds:  30,

					}
					log.Printf("The new created readiness probe: %v", pod.Spec.Containers[i].ReadinessProbe)

				}
			}
		} else {
			log.Print("No 'notebook-name' label, so not adding readiness probe")
		}
	}

	return pod
}

// mutatePods implements the logic of our admission controller webhook.
// For every pod that is created some actions can be taken.
// Now, only the readiness probe is added to the notebook container in the notebook pod
//func applySecurityDefaults(req *v1beta1.AdmissionRequest) ([]patchOperation, error) {
//func applySecurityDefaults(req *v1beta1.AdmissionRequest) ([]jsonpatch.JsonPatchOperation, error) {
func mutatePods(req *v1beta1.AdmissionRequest) ([]jsonpatch.JsonPatchOperation, error) {
	// This handler should only get called on Pod objects as per the MutatingWebhookConfiguration in the YAML file.
	// However, if (for whatever reason) this gets invoked on an object of a different kind, issue a log message but
	// let the object request pass through otherwise.
	if req.Resource != podResource {
		log.Printf("expect resource to be %s", podResource)
		return nil, nil
	}

	// Parse the Pod object.
	raw := req.Object.Raw
	pod := corev1.Pod{}
	if _, _, err := universalDeserializer.Decode(raw, nil, &pod); err != nil {
		return nil, fmt.Errorf("could not deserialize pod object: %v", err)
	}

	podCopy := pod.DeepCopy() // new added
	log.Printf("Examining pod: %v\n", pod.GetName())

	// Take a look at the annotations
	// if podAnnotations := pod.GetAnnotations(); podAnnotations != nil {
	// 	log.Printf("Looking at pod annotations, found: %v", podAnnotations)
	// } else {
	// 	log.Print("Pod has no annotations")
	// }

	// Take a look at the labels
	if podLabels := pod.GetLabels(); podLabels != nil {
		log.Printf("Looking at pod labels, found: %v", podLabels)
	} else {
		log.Print("Pod has no labels")
	}

	pod = addReadinessProbeToNotebook(pod)

	podJSON, _ := json.Marshal(pod) // TODO error handling
	podCopyJSON, _ := json.Marshal(podCopy)
	jsonPatch, _ := jsonpatch.CreatePatch(podCopyJSON, podJSON)

	return jsonPatch, nil
}

func main() {
	certPath := filepath.Join(tlsDir, tlsCertFile)
	keyPath := filepath.Join(tlsDir, tlsKeyFile)

	mux := http.NewServeMux()
	mux.Handle("/mutate", admitFuncHandler(mutatePods))
	server := &http.Server{
		// We listen on port 8443 such that we do not need root privileges or extra capabilities for this server.
		// The Service object will take care of mapping this port to the HTTPS port 443.
		Addr:    ":8443",
		Handler: mux,
	}
	log.Fatal(server.ListenAndServeTLS(certPath, keyPath))
}
