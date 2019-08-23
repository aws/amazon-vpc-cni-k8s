package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	received = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cni_test_received_total",
		Help: "Number of events received",
	})

	dnsRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cni_test_dns_request_total",
		Help: "Number of dns requests sent",
	})
	dnsRequestFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cni_test_dns_request_failure",
		Help: "Number of dns request failures",
	})

	externalHTTPRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cni_test_external_http_request_total",
		Help: "Number of external http requests sent",
	})
	externalHTTPRequestFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cni_test_external_http_request_failure",
		Help: "Number of external http request failures",
	})

	svcClusterIPRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cni_test_cluster_ip_request_total",
		Help: "Number of requests set to service's cluster IP",
	})
	svcClusterIPRequestFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cni_test_cluster_ip_request_failure",
		Help: "Number of requests that failed to reach cluster IP",
	})

	svcPodIPRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cni_test_pod_ip_request_total",
		Help: "Number of requests set to service's pod IP",
	})
	svcPodIPRequestFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cni_test_pod_ip_request_failure",
		Help: "Number of requests that failed to reach pod IP",
	})

	requests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cni_test_request_total",
		Help: "Number of total requests",
	}, []string{"pod_name", "pod_ip", "host_ip"})
	requestFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cni_test_request_failure",
		Help: "Number of successful requests",
	}, []string{"pod_name", "pod_ip", "host_ip"})
)

var (
	namespace = "cni-test"
)

func runServer() {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, "ok\n")
	})
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/test", func(w http.ResponseWriter, req *http.Request) {
		received.Inc()
		io.WriteString(w, "ok\n")
	})
	s := http.Server{
		Addr:         ":8080",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	log.Fatal(s.ListenAndServe())
}

// runTest checks whether each running pod in the cluster can hit the test endpoint on the pod,
// if it can do a DNS lookup, and if the cluster and pod services can be reached
func runTest() {
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	for {
		pods, err := clientset.CoreV1().Pods(namespace).List(metav1.ListOptions{
			LabelSelector: "app=testpod",
		})

		if err != nil {
			log.Print(err)
			continue
		}

		readyPods := 0

		fmt.Printf("There are %d pods in the cluster\n", len(pods.Items))
		for _, pod := range pods.Items {
			if pod.Status.Phase != "Running" {
				log.Printf("Skipping pod %s in phase %s\n", pod.Name, pod.Status.Phase)
				continue
			}
			for _, containerCondition := range pod.Status.Conditions {
				if containerCondition.Type == corev1.ContainersReady {
					if containerCondition.Status == corev1.ConditionTrue {
						readyPods++
					}
				}

			}
			log.Printf("%+v\n\n", pod)
			counter := requests.WithLabelValues(pod.Name, pod.Status.PodIP, pod.Status.HostIP)
			failure := requestFailures.WithLabelValues(pod.Name, pod.Status.PodIP, pod.Status.HostIP)
			resp, err := http.Get(fmt.Sprintf("http://%s:8080/test", pod.Status.PodIP))
			counter.Inc()
			if err != nil {
				log.Printf("http error: %+v\n", err)
				failure.Inc()
				continue
			}
			data, err := ioutil.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				log.Printf("Read error: %+v\n", err)
				failure.Inc()
				continue
			}
			s := string(data)
			if s != "ok\n" {
				log.Printf("Data was not 'ok': %s\n", s)
				failure.Inc()
				continue
			}
		}

		log.Printf("Found %d ready pods\n", readyPods)

		dnsRequests.Inc()
		_, err = net.LookupIP("aws.amazon.com")
		if err != nil {
			log.Printf("dns lookup error: %+v\n", err)
			dnsRequestFailures.Inc()
		}

		externalHTTPRequests.Inc()
		httpClient := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
		_, err = httpClient.Get("https://aws.amazon.com")
		if err != nil {
			log.Printf("http request to aws.amazon.com failed: %+v\n", err)
			externalHTTPRequestFailures.Inc()
		}

		// Ensure that service is ready
		clusterEndpoints, err := clientset.CoreV1().Endpoints(namespace).Get("testpod-clusterip", metav1.GetOptions{})
		if err != nil {
			if err != nil {
				log.Print(err)
				continue
			}
		}
		count := 0
		for _, subset := range clusterEndpoints.Subsets {
			count += len(subset.Addresses)
		}
		if count >= len(pods.Items) {
			svcClusterIPRequests.Inc()
			resp, err := http.Get("http://testpod-clusterip.cni-test.svc.cluster.local:8080/healthz")
			if err != nil {
				log.Printf("http request to testpod-clusterip failed: %+v\n", err)
				svcClusterIPRequestFailures.Inc()
			} else {
				resp.Body.Close()
			}
		}
		// Ensure that service is ready
		podEndpoints, err := clientset.CoreV1().Endpoints(namespace).Get("testpod-pod-ip", metav1.GetOptions{})
		if err != nil {
			log.Print(err)
			continue
		}
		count = 0
		for _, subset := range podEndpoints.Subsets {
			count += len(subset.Addresses)
		}
		if count >= len(pods.Items) {
			svcPodIPRequests.Inc()
			resp, err := http.Get("http://testpod-pod-ip.cni-test.svc.cluster.local:8080/healthz")
			if err != nil {
				log.Printf("http request to testpod-pod-ip failed: %+v\n\n", err)
				svcPodIPRequestFailures.Inc()
			} else {
				resp.Body.Close()
			}
		}

		time.Sleep(time.Second)
	}

}

func main() {
	c := make(chan bool)
	go runServer()
	go runTest()
	<-c
}
