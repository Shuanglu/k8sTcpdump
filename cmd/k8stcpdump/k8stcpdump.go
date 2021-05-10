package k8stcpdump

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	log "github.com/sirupsen/logrus"

	//"9fans.net/go/plan9/client"

	apicore "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	//"k8s.io/client-go/util/homedir"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	homedir "github.com/mitchellh/go-homedir"

	"crypto/rand"
	"encoding/hex"
	"io"
	"io/ioutil"
	"sync"
	"time"
	//"k8sTcpdump/k8stcpdump"
)

var cfgFile string

//var parFile string

type targetPod struct {
	Name      string `json:"Name"`
	Namespace string `json:"Namespace"`
	Node      string `json:"Node",omitempty`
	Uid       string `json:"Uid", omitempty`
}

type targetPods struct {
	Pods            []targetPod `json:"Pods"`
	Runtime         string      `json:"Runtime"`
	RuntimeEndpoint string      `json:"RuntimeEndpoint"`
	Duration        int         `json:"Duration"`
}

type runtime struct {
	Name            string `json:"Name"`
	RuntimeEndpoint string `json:"RuntimeEndpoint"`
}

type targets struct {
	Pods     []targetPod `json:"Pods"`
	Runtimes []runtime   `json:"Runtimes,omitempty"`
	//RuntimeEndpoint map[string]string `json:"RuntimeEndpoint,omitempty"`
	Duration int `json:"Duration"`
	//Deployments  []target `json:"Deployments"`
	//Daemonsets   []target `json:"Daemonsets"`
	//Replicasets  []target `json:"Replicasets"`
	//Statefulsets []target `json:"Statefulsets"`
}

func filterPods(client *kubernetes.Clientset, targetpods []targetPod) ([]targetPod, []targetPod, map[string][]string) {
	var dockerFilteredTargetPods, containerdFilteredTargetPods []targetPod
	nodeMap := make(map[string][]string)
	for _, targetpod := range targetpods {
		pod, err := client.CoreV1().Pods(targetpod.Namespace).Get(context.TODO(), targetpod.Name, metav1.GetOptions{})
		if err != nil {
			log.Warning(fmt.Sprintf("Failed to get the pod %s in the namespace %s", targetpod.Name, targetpod.Namespace))
			continue
		}
		if pod.Status.Phase == "Pending" || pod.Status.Phase == "Unknown" || pod.Status.Phase == "Failed" {
			log.Warning("Pod %s in the namespace %s is not in running/crashloopbackoff status. Will skip this pod", pod.Name, pod.Namespace)
		}
		nodeInfo, err := client.CoreV1().Nodes().Get(context.TODO(), pod.Spec.NodeName, metav1.GetOptions{})
		if err != nil {
			log.Warning(fmt.Sprintf("Failed to get the node info of the pod %s in the namespace %s: %s. Will skip this pod", targetpod.Name, targetpod.Namespace, err))
			continue
		}
		filteredTargetPod := targetPod{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Uid:       string(pod.UID),
			Node:      nodeInfo.Name,
		}

		containerRumtime := nodeInfo.Status.NodeInfo.ContainerRuntimeVersion
		docker, err := regexp.Match("docker", []byte(containerRumtime))
		if err != nil {
			log.Warning(fmt.Sprintf("Failed to get the container runtime version of the node %s: %s. Will skip this pod", nodeInfo.Name, err))
			continue
		}
		if docker {
			dockerFilteredTargetPods = append(dockerFilteredTargetPods, filteredTargetPod)
			nodeMap["docker"] = append(nodeMap["docker"], nodeInfo.Name)
		}
		containerd, _ := regexp.Match("containerd", []byte(containerRumtime))
		if containerd {
			containerdFilteredTargetPods = append(containerdFilteredTargetPods, filteredTargetPod)
			nodeMap["containerd"] = append(nodeMap["containerd"], nodeInfo.Name)
		}
	}
	return dockerFilteredTargetPods, containerdFilteredTargetPods, nodeMap
}

func parse(p string) (*rest.Config, *kubernetes.Clientset, targets, error) {
	if cfgFile == "" {
		home, _ := homedir.Dir()
		cfgFile = filepath.Join(home, ".kube", "config")
		log.Info(fmt.Sprintf("Will use the kubeconfig under path %s", cfgFile))
	}
	restConfig, err := clientcmd.BuildConfigFromFlags("", cfgFile)
	if err != nil {
		err = fmt.Errorf("Failed to build k8s REST client using the kubeconfig file under %s", cfgFile)
		return nil, nil, targets{}, err
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		err = fmt.Errorf("Failed to build k8s clientset using the kubeconfig file under path %s", cfgFile)
		return nil, nil, targets{}, err
	}

	file, err := ioutil.ReadFile(p)
	if err != nil {
		err = fmt.Errorf("Failed to load the paramter file under path %s: %s", p, err)
		return nil, nil, targets{}, err
	}

	data := targets{}
	err = json.Unmarshal([]byte(file), &data)
	if err != nil {
		err = fmt.Errorf("Failed to load the JSON data from the parameter file %s: ", p, err)
		return nil, nil, targets{}, err
	}
	return restConfig, client, data, nil
}

func generatePodManifest(runtimeEndpoint string, configMapName string, labelValue string, podSuffix string, node string) apicore.Pod {
	boolValue := true
	hostpathType := v1.HostPathSocket
	return apicore.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tcpdumpagent" + "-" + podSuffix,
			Namespace: "default",
			Labels:    map[string]string{"run": labelValue},
		},
		Spec: apicore.PodSpec{
			HostNetwork: true,
			Volumes: []apicore.Volume{
				{
					Name: "targetpodsjson",
					VolumeSource: apicore.VolumeSource{
						ConfigMap: &apicore.ConfigMapVolumeSource{
							LocalObjectReference: apicore.LocalObjectReference{
								Name: configMapName,
							},
						},
					},
				},
				{
					Name: "var",
					VolumeSource: apicore.VolumeSource{
						HostPath: &apicore.HostPathVolumeSource{
							Path: "/var/run/",
						},
					},
				},
				{
					Name: "runtimeendpoint",
					VolumeSource: apicore.VolumeSource{
						HostPath: &apicore.HostPathVolumeSource{
							Path: runtimeEndpoint,
							Type: &hostpathType,
						},
					},
				},
			},
			Containers: []apicore.Container{
				{
					Name:  "tcpdumpagent",
					Image: "shawnlu/tcpdumpagent:20210429",
					VolumeMounts: []apicore.VolumeMount{
						{
							Name:      "var",
							MountPath: "/var/run/",
						},
						{
							Name:      "targetpodsjson",
							MountPath: "/mnt/",
						},
						{
							Name:      "runtimeendpoint",
							MountPath: runtimeEndpoint,
						},
					},
					ReadinessProbe: &apicore.Probe{
						Handler: apicore.Handler{
							Exec: &apicore.ExecAction{
								Command: []string{"ls", "/tmp/tcpdumpAgentComplete"},
							},
						},
					},
					SecurityContext: &apicore.SecurityContext{
						Privileged: &boolValue,
					},
				},
			},
			NodeSelector: map[string]string{"kubernetes.io/hostname": node},
		},
	}
}

func watchPodStatus(client *kubernetes.Clientset, tcpdumpPod *apicore.Pod) wait.ConditionFunc {
	return func() (bool, error) {
		wpod, err := client.CoreV1().Pods(tcpdumpPod.ObjectMeta.Namespace).Get(context.TODO(), tcpdumpPod.ObjectMeta.Name, metav1.GetOptions{})
		if err != nil {
			log.Warn(fmt.Sprintf("Failed to get the pod '%s' in the namespace '%s'. Will exit.", tcpdumpPod.ObjectMeta.Name, tcpdumpPod.ObjectMeta.Namespace))

			//return false, err
		}
		if wpod.Status.Phase == "Running" {
			if wpod.Status.Conditions[1].Type == apicore.PodReady {
				if wpod.Status.Conditions[1].Status == apicore.ConditionTrue {
					log.Info(fmt.Sprintf("Tcpdump process in the Pod '%s' in the namesapce '%s' has completed now", tcpdumpPod.ObjectMeta.Name, tcpdumpPod.ObjectMeta.Namespace))
					return true, nil
				}
			}
		}
		return false, nil
	}
}

func createPod(client *kubernetes.Clientset, podManifest *apicore.Pod) (*apicore.Pod, error) {
	//podDefinition := getPodDef(targetPod, duration)
	tcpdumpPod, err := client.CoreV1().Pods(podManifest.ObjectMeta.Namespace).Create(context.TODO(), podManifest, metav1.CreateOptions{})
	if err == nil {
		log.Info(fmt.Sprintf("Pod '%s' in the namespace '%s' has been created.", tcpdumpPod.Name, tcpdumpPod.Namespace))
	} else {
		log.Warn(fmt.Sprintf("Pod '%s' in the namespace '%s' failed to be created due to '%s'.", podManifest.ObjectMeta.Name, podManifest.ObjectMeta.Namespace, err.Error()))
	}
	return tcpdumpPod, err
}

func downloadFromPod(restConfig *rest.Config, client *kubernetes.Clientset, tcpdumpPod *apicore.Pod, pods []string) error {
	//path := "/tmp/" + tcpdumpPod.ObjectMeta.Namespace + "-" + tcpdumpPod.ObjectMeta.Name + ".cap"

	for _, file := range pods {
		command := []string{"tar", "cf", "-", "/tmp/tcpdumpagent/" + file + ".cap"}
		req := client.CoreV1().RESTClient().Post().Namespace(tcpdumpPod.ObjectMeta.Namespace).Resource("pods").Name(tcpdumpPod.ObjectMeta.Name).SubResource("exec").VersionedParams(&apicore.PodExecOptions{
			Container: tcpdumpPod.Spec.Containers[0].Name,
			Command:   command,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)
		exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
		if err != nil {
			log.Warn(fmt.Sprintf("Failed to build stream connection with the Pod '%s'.", tcpdumpPod.ObjectMeta.Name))
			log.Fatal(err)
		}
		reader, outStream := io.Pipe()

		go func() {
			defer outStream.Close()
			err = exec.Stream(remotecommand.StreamOptions{
				Stdin:  os.Stdin,
				Stdout: outStream,
				Stderr: os.Stderr,
				Tty:    false,
			})
		}()
		tarReader := tar.NewReader(reader)
		for {
			_, err := tarReader.Next()
			if err != nil {
				if err != io.EOF {
					log.Warn(fmt.Sprintf("The tar file in the pod '%s' doesn't end with EOF", tcpdumpPod.ObjectMeta.Name))
					log.Fatal(err)
					return err
				}
				break
			}
			destFileName := "./" + file + ".cap"
			outFile, err := os.Create(destFileName)
			if err != nil {
				log.Warn(fmt.Sprintf("Error while creating the local dump file for pod '%s'", tcpdumpPod.ObjectMeta.Name))
			}
			defer outFile.Close()
			if _, err := io.Copy(outFile, tarReader); err != nil {
				log.Warn(fmt.Sprintf("Failed to copy the file %s due to '%s'", destFileName, err.Error()))
				return err
			}
			if err := outFile.Close(); err != nil {
				log.Warn(fmt.Sprintf("Failed to close the file %s due to '%s'", destFileName, err.Error()))
				return err
			}
		}
	}

	return nil
}

func cleanUp(client *kubernetes.Clientset, tcpdumpPod *apicore.Pod) error {
	log.Info(fmt.Sprintf("Cleanup the Pod '%s' in the namespace '%s'", tcpdumpPod.ObjectMeta.Name, tcpdumpPod.ObjectMeta.Namespace))
	//var GracePeriodSeconds int64
	//GracePeriodSeconds = 0
	err := client.CoreV1().Pods(tcpdumpPod.ObjectMeta.Namespace).Delete(context.TODO(), tcpdumpPod.ObjectMeta.Name, metav1.DeleteOptions{})
	return err
}

func podOperation(workerGroup *sync.WaitGroup, restConfig *rest.Config, client *kubernetes.Clientset, podManifest apicore.Pod, duration int, podOperationErr map[string]error, pods []string) {
	defer workerGroup.Done()
	var tcpdumpPod *apicore.Pod
	var err error
	for i := 0; i < 5; i++ {
		tcpdumpPod, err = createPod(client, &podManifest)
		if err == nil || i == 4 {
			break
		}
		time.Sleep(time.Duration(10))
	}
	//tcpdumpPod, err := retry(5,time.Duration(10), createPod, parameters)
	if err != nil {
		//retry once more if any error
		//tcpdumpPod, err = createPod(client, &podManifest)
		podOperationErr[podManifest.Spec.NodeSelector["kubernetes.io/hostname"]] = err
	}
	if err == nil {
		durationStr := strconv.Itoa(duration+300) + "s"
		durationWait, _ := time.ParseDuration(durationStr)
		err = wait.PollImmediate(time.Second*1, time.Duration(durationWait), watchPodStatus(client, tcpdumpPod))
		if err != nil {
			log.Warn(fmt.Sprintf("Timeout while waiting tcpdump for pod '%s' in the namespace '%s' to complete: %s", tcpdumpPod.ObjectMeta.Name, tcpdumpPod.ObjectMeta.Namespace, err))
			//log.Fatal(err)
		} else {
			err = downloadFromPod(restConfig, client, tcpdumpPod, pods)
			if err != nil {
				log.Warn(fmt.Sprintf("Failed to download dump file from pod '%s' in the namespace '%s': %s", tcpdumpPod.ObjectMeta.Name, tcpdumpPod.ObjectMeta.Namespace, err))
			}
		}
		err = cleanUp(client, tcpdumpPod)
		if err != nil {
			log.Warn(fmt.Sprintf("Failed to clean up the pod '%s'", tcpdumpPod.ObjectMeta.Name))
		}
	}
	//return nil
}

// Run is responsible for starting the command
func Run(parFile string) {
	//Parse the parameter file which contains the pods to be captured.
	restConfig, client, inputTargets, err := parse(parFile)
	duration := inputTargets.Duration
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to parse the parameter file under %s: %s", parFile, err))
	}
	if duration <= 0 {
		log.Fatal("Duration has to be >= 0")
	}
	var dockerFilteredTargetPods, containerdFilteredTargetPods []targetPod
	var nodeMap map[string][]string
	//Create maping of pods/nodes
	dockerFilteredTargetPods, containerdFilteredTargetPods, nodeMap = filterPods(client, inputTargets.Pods)
	if len(dockerFilteredTargetPods) == 0 && len(containerdFilteredTargetPods) == 0 {
		log.Fatal(fmt.Sprintf("No pods are available to capture the network trace. Please check previous log. Exiting....."))
	}
	var containerdEndpoint, dockerEndpoint string
	containerdEndpoint = "/var/run/containerd/containerd.sock"
	dockerEndpoint = "/var/run/dockershim.sock"
	for _, runtime := range inputTargets.Runtimes {
		log.Info(fmt.Sprintf("Runtime name is: %s and endpoint is: %s", runtime.Name, runtime.RuntimeEndpoint))
		if runtime.Name == "docker" {
			dockerEndpoint = runtime.RuntimeEndpoint
			log.Info(fmt.Sprintf("endpoint of docker is %s: ", dockerEndpoint))
		}
		if runtime.Name == "containerd" {
			containerdEndpoint = runtime.RuntimeEndpoint
			log.Info(fmt.Sprintf("endpoint of containerd is: %s", containerdEndpoint))
		}
	}
	temp := make([]byte, 2)
	rand.Read(temp)
	suffix := hex.EncodeToString(temp)
	var containerdConfigMapName, dockerConfigMapName string
	var containerdErr, dockerErr error
	labelValue := "k8stcpdump" + suffix
	var workerGroup sync.WaitGroup
	if len(containerdFilteredTargetPods) > 0 {
		containerdConfigMapName = "containerdconfigmap-" + suffix

		containerdConfigMapData := targetPods{
			Pods:            containerdFilteredTargetPods,
			Duration:        inputTargets.Duration,
			Runtime:         "containerd",
			RuntimeEndpoint: containerdEndpoint,
		}
		containerConfigMapDataByte, err := json.Marshal(containerdConfigMapData)
		if err == nil {
			containerdConfigMap := &apicore.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      containerdConfigMapName,
					Namespace: "default",
					Labels:    map[string]string{"run": labelValue},
				},
				Data: map[string]string{"pods.json": string(containerConfigMapDataByte)},
			}
			for i := 0; i < 5; i++ {
				_, err := client.CoreV1().ConfigMaps("default").Create(context.TODO(), containerdConfigMap, metav1.CreateOptions{})
				if err == nil || i == 4 {
					break
				}
				time.Sleep(time.Duration(10))
			}
			if err != nil {
				//containerdErr = fmt.Errorf("Failed to create configmap %s: %s", containerdConfigMapName, err)
				log.Fatal(fmt.Sprintf("Failed to create configmap %s: %s", containerdConfigMapName, err))
			}
		} else {
			//containerdErr = fmt.Errorf("Failed to create configmap %s: %s", containerdConfigMapName, err)
			log.Fatal(fmt.Sprintf("Failed to create configmap %s: %s", containerdConfigMapName, err))
		}
	}
	if len(dockerFilteredTargetPods) > 0 {
		dockerConfigMapName = "dockerconfigmap-" + suffix

		dockerdConfigMapData := targetPods{
			Pods:            dockerFilteredTargetPods,
			Duration:        inputTargets.Duration,
			Runtime:         "docker",
			RuntimeEndpoint: dockerEndpoint,
		}
		dockerdConfigMapDataByte, err := json.Marshal(dockerdConfigMapData)
		if err == nil {
			dockerConfigMap := &apicore.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dockerConfigMapName,
					Namespace: "default",
					Labels:    map[string]string{"run": labelValue},
				},
				Data: map[string]string{"pods.json": string(dockerdConfigMapDataByte)},
			}
			for i := 0; i < 5; i++ {
				_, err := client.CoreV1().ConfigMaps("default").Create(context.TODO(), dockerConfigMap, metav1.CreateOptions{})
				if err == nil || i == 4 {
					break
				}
				time.Sleep(time.Duration(10))
			}
			if err != nil {
				//dockerErr = fmt.Errorf("Failed to create configmap %s: %s", dockerConfigMapName, err)
				log.Fatal(fmt.Sprintf("Failed to create configmap %s: %s", dockerConfigMapName, err))
			}
		} else {
			//dockerErr = fmt.Errorf("Failed to create configmap %s: %s", dockerConfigMapName, err)
			log.Fatal(fmt.Sprintf("Failed to create configmap %s: %s", dockerConfigMapName, err))
		}
	}
	podOperationErr := make(map[string]error)
	podManifest := apicore.Pod{}
	podManifests := []apicore.Pod{}
	existingNodes := make(map[string]int)
	if len(containerdFilteredTargetPods) > 0 && containerdErr == nil {
		for _, node := range nodeMap["containerd"] {
			if existingNodes[node] == 0 {
				existingNodes[node] = 1
				temp := make([]byte, 2)
				rand.Read(temp)
				podSuffix := hex.EncodeToString(temp)
				podManifest = generatePodManifest(containerdEndpoint, containerdConfigMapName, labelValue, podSuffix, node)
				podManifests = append(podManifests, podManifest)
			}
		}
	}
	if len(dockerFilteredTargetPods) > 0 && dockerErr == nil {
		for _, node := range nodeMap["docker"] {
			if existingNodes[node] == 0 {
				existingNodes[node] = 1
				temp := make([]byte, 2)
				rand.Read(temp)
				podSuffix := hex.EncodeToString(temp)
				podManifest = generatePodManifest(dockerEndpoint, dockerConfigMapName, labelValue, podSuffix, node)
				podManifests = append(podManifests, podManifest)
			}
		}
	}
	for _, pod := range podManifests {
		workerGroup.Add(1)
		pods := []string{}
		for _, containerdFilteredTargetPod := range containerdFilteredTargetPods {
			if pod.Spec.NodeSelector["kubernetes.io/hostname"] == containerdFilteredTargetPod.Node {
				pods = append(pods, containerdFilteredTargetPod.Namespace+"-"+containerdFilteredTargetPod.Name)
			}
		}
		for _, dockerFilteredTargetPod := range dockerFilteredTargetPods {
			if pod.Spec.NodeSelector["kubernetes.io/hostname"] == dockerFilteredTargetPod.Node {
				pods = append(pods, dockerFilteredTargetPod.Namespace+"-"+dockerFilteredTargetPod.Name)
			}
		}
		go podOperation(&workerGroup, restConfig, client, pod, duration, podOperationErr, pods)
	}
	//fmt.Println("Wait for workers")
	workerGroup.Wait()
	//fmt.Println("All workers have completed")
	err = client.CoreV1().ConfigMaps("default").DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "run=" + labelValue,
	})
	log.Info(fmt.Sprintf("Cleanup the Configmaps with label: %s", "run="+labelValue))
	if err != nil {
		log.Error(fmt.Sprintf("Failed to clean up the configmaps due to: %s", err))
	}
	log.Info("All operations have been completed. EXIT now.")
}
