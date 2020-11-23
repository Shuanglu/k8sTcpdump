package k8stcpdump

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"

	//"9fans.net/go/plan9/client"

	apicore "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
	"strconv"
	"sync"
	"time"
	//"k8sTcpdump/k8stcpdump"
)

var cfgFile string
var parFile string

type target struct {
	Name      string `json:"Name"`
	Namespace string `json:"Namespace"`
	Node      string `json:"Node"`
	Uid       string `json:"Uid"`
}

type targets struct {
	Pods     []target `json:"Pods"`
	Duration string   `json:"Duration"`
	//Deployments  []target `json:"Deployments"`
	//Daemonsets   []target `json:"Daemonsets"`
	//Replicasets  []target `json:"Replicasets"`
	//Statefulsets []target `json:"Statefulsets"`
}

type targetPods struct {
	Pods []target `json:"Pods"`
}

func dropErr(e error) {
	if e != nil {
		panic(e)
	}
}

func getPodStatus(client *kubernetes.Clientset, data *targets) *[]target {
	var targetPods []target
	for i := 0; i < len(data.Pods); i++ {
		pod, err := client.CoreV1().Pods(data.Pods[i].Namespace).Get(context.TODO(), data.Pods[i].Name, metav1.GetOptions{})
		podName := pod.ObjectMeta.Name
		podStatus := pod.Status.Phase
		podNode := pod.Spec.NodeName
		podUid := string(pod.ObjectMeta.UID)
		if errors.IsNotFound(err) {
			log.Info(fmt.Sprintf("Pod '%s' in namespace '%s' not found. Will skip this pod", data.Pods[i].Name, data.Pods[i].Namespace))
		} else if statusError, isStatus := err.(*errors.StatusError); isStatus {
			log.Warn(fmt.Sprintf("Error getting pod '%s' in namespace '%s': %v. Will skip this pod.",
				data.Pods[i].Name, data.Pods[i].Namespace, statusError.ErrStatus.Message))
		} else if err != nil {
			log.Fatal("All target pods are not able to be found", err)
		} else {
			if podStatus != "Pending" && podStatus != "Unkonwn" && podStatus != "Failed" {
				log.Info(fmt.Sprintf("Pod '%s' in the namespace '%s' has been scheduled on the %s and not in 'Failed/Unknown' status", podName, data.Pods[i].Namespace, podNode))
				if data.Pods[i].Node != podNode {
					log.Info(fmt.Sprintf("Node of Pod '%s' in the namespace '%s' is different/missing from the input. Use the latest one %s", podName, data.Pods[i].Namespace, podNode))
				}
				var targetPod target
				targetPod.Name = podName
				targetPod.Node = podNode
				targetPod.Namespace = pod.Namespace
				targetPod.Uid = podUid
				targetPods = append(targetPods, targetPod)
			}
		}
	}
	return &targetPods
}

func parse(p string) (*rest.Config, *kubernetes.Clientset, *targets) {
	if cfgFile == "" {
		home, _ := homedir.Dir()
		cfgFile = filepath.Join(home, ".kube", "config")
		log.Info(fmt.Sprintf("Load the kubeconfig under path '%s'", cfgFile))
	}
	restConfig, err := clientcmd.BuildConfigFromFlags("", cfgFile)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to build k8s REST client using the kubeconfig file under '%s'", cfgFile))
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to build k8s clientset using the kubeconfig file under path '%s'", cfgFile))
	}

	file, err := ioutil.ReadFile(p)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to load the paramter file under path '%s'", p))
	}

	data := targets{}
	err = json.Unmarshal([]byte(file), &data)
	if err != nil {
		log.Fatal("Failed to load the JSON data from the parameter file")
	}
	return restConfig, client, &data
}

func getPodDef(pod *target, duration string) *apicore.Pod {
	n := 2
	temp := make([]byte, n)
	rand.Read(temp)
	suffix := hex.EncodeToString(temp)
	var privileged bool
	privileged = true
	var command string
	var probeCommand []string
	command = "rm -rf /tmp/" + pod.Name + "_" + pod.Namespace + ".cap; rm -rf /tmp/complete-" + pod.Name + "_" + pod.Namespace + "; nsenter -t $(docker inspect $(docker ps |grep '" + pod.Uid + "'|grep -v pause|awk '{print $1}')| grep '\"Pid\":' | grep -Eo '[0-9]*') -n timeout " + duration + " tcpdump -i any -w /tmp/" + pod.Name + "_" + pod.Namespace + ".cap; sleep 2;touch /tmp/complete-" + pod.Name + "_" + pod.Namespace + "; tail -f /dev/null"
	//log.Info(command)
	probeCommand = []string{"ls", "/tmp/complete-" + pod.Name + "_" + pod.Namespace}
	//log.Info(probeCommand)
	return &apicore.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name + "-" + suffix,
			Namespace: pod.Namespace,
			Labels: map[string]string{
				"tdn":  pod.Name,
				"tdns": pod.Namespace,
			},
		},
		Spec: apicore.PodSpec{
			Containers: []apicore.Container{
				{
					Name:            pod.Name,
					Image:           "docker.io/library/alpine",
					ImagePullPolicy: apicore.PullIfNotPresent,
					Command: []string{
						"nsenter",
						"-t",
						"1",
						"-m",
						"-u",
						"-i",
						"-n",
						"-p",
						"--",
						"bash",
						"-c",
						command,
					},
					SecurityContext: &apicore.SecurityContext{
						Privileged: &privileged,
					},
					ReadinessProbe: &apicore.Probe{
						Handler: apicore.Handler{
							Exec: &apicore.ExecAction{
								Command: probeCommand,
							},
						},
					},
				},
			},
			NodeName: pod.Node,
			HostPID:  true,
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

func createPod(client *kubernetes.Clientset, targetPod *target, duration string) (*apicore.Pod, error) {
	podDefinition := getPodDef(targetPod, duration)
	tcpdumpPod, err := client.CoreV1().Pods(podDefinition.ObjectMeta.Namespace).Create(context.TODO(), podDefinition, metav1.CreateOptions{})
	if err == nil {
		log.Info(fmt.Sprintf("Pod '%s' in the namespace '%s' has been created.", tcpdumpPod.Name, tcpdumpPod.Namespace))
	} else {
		log.Warn(fmt.Sprintf("Pod '%s' in the namespace '%s' failed to be created due to '%s'.", podDefinition.ObjectMeta.Name, podDefinition.ObjectMeta.Namespace, err.Error()))
	}
	return tcpdumpPod, err
}

func downloadFromPod(restConfig *rest.Config, client *kubernetes.Clientset, tcpdumpPod *apicore.Pod) error {
	path := "/tmp/" + tcpdumpPod.Spec.Containers[0].Name + "_" + tcpdumpPod.ObjectMeta.Namespace + ".cap"
	command := []string{"tar", "cf", "-", path}
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
		destFileName := "./" + tcpdumpPod.Spec.Containers[0].Name + "-" + tcpdumpPod.ObjectMeta.Namespace + ".cap"
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
	return err
}

func cleanUp(client *kubernetes.Clientset, tcpdumpPod *apicore.Pod) error {
	log.Info(fmt.Sprintf("Cleanup the Pod '%s' in the namespace '%s'", tcpdumpPod.ObjectMeta.Name, tcpdumpPod.ObjectMeta.Namespace))
	//var GracePeriodSeconds int64
	//GracePeriodSeconds = 0
	err := client.CoreV1().Pods(tcpdumpPod.ObjectMeta.Namespace).Delete(context.TODO(), tcpdumpPod.ObjectMeta.Name, metav1.DeleteOptions{})
	return err
}

//func watchPodStatus(client *kubernetes.Clientset, tcpdumpPod *apicore.Pod) bool {
//	listOption := "metadata.name=" + tcpdumpPod.ObjectMeta.Name
//	podWatcher, err := client.CoreV1().Pods(tcpdumpPod.ObjectMeta.Namespace).Watch(context.TODO(), metav1.ListOptions{
//		FieldSelector: listOption})
//	if err != nil {
//		log.Warn(fmt.Sprintf("Failed to get the status of the pod '%s' in the namespace '%s'", tcpdumpPod.ObjectMeta.Name, tcpdumpPod.ObjectMeta.Namespace))
//	}
//var podWatcherRes struct
//	podWatcherEvent := <-podWatcher.ResultChan()
//	log.Info(podWatcherEvent.Type)
//	if podWatcherEvent.Type == "MODIFIED" {
//		return true
//	}
//	return false
//}

func podOperation(workerGroup *sync.WaitGroup, restConfig *rest.Config, client *kubernetes.Clientset, targetPod *target, duration string, sleepTime time.Duration) error {
	defer workerGroup.Done()
	tcpdumpPod, err := createPod(client, targetPod, duration)
	if err == nil {

		err = wait.PollImmediate(time.Second*1, sleepTime, watchPodStatus(client, tcpdumpPod))
		//for {
		//if modified := watchPodStatus(client, tcpdumpPod); modified == true {
		//	tcpdumpPod, err := client.CoreV1().Pods(tcpdumpPod.ObjectMeta.Namespace).Get(context.TODO(), tcpdumpPod.ObjectMeta.Name, metav1.GetOptions{})
		//	if err != nil {
		//		log.Warn(fmt.Sprintf("Failed to get the pod '%s' in the namespace '%s'. Will EXIT.", tcpdumpPod.ObjectMeta.Name, tcpdumpPod.ObjectMeta.Namespace))
		//		log.Fatal(err)
		//return false, err
		//	}
		//	if tcpdumpPod.Status.Phase == "Running" {
		//		if tcpdumpPod.Status.Conditions[1].Type == apicore.PodReady {
		//			if tcpdumpPod.Status.Conditions[1].Status == apicore.ConditionTrue {
		//				log.Info(fmt.Sprintf("Tcpdump process in the Pod '%s' in the namesapce '%s' has completed now", tcpdumpPod.ObjectMeta.Name, tcpdumpPod.ObjectMeta.Namespace))
		//				break
		//			}
		//		}
		//	}
		//}
		//}

		if err != nil {
			log.Warn(fmt.Sprintf("Timeout while waiting tcpdump for pod '%s' in the namespace '%s' to complete", tcpdumpPod.ObjectMeta.Name, tcpdumpPod.ObjectMeta.Namespace))
			//log.Fatal(err)
		} else {
			err = downloadFromPod(restConfig, client, tcpdumpPod)
			if err != nil {
				log.Warn(fmt.Sprintf("Failed to download dump file from pod '%s' in the namespace '%s'", tcpdumpPod.ObjectMeta.Name, tcpdumpPod.ObjectMeta.Namespace))
			}
		}
		err = cleanUp(client, tcpdumpPod)
		if err != nil {
			log.Warn(fmt.Sprintf("Failed to clean up the pod '%s'", tcpdumpPod.ObjectMeta.Name))
		}
	}

	return nil
}

// Run is responsible for starting the command
func Run(parFile string) {
	restConfig, client, targetPodsp := parse(parFile)
	duration := *&targetPodsp.Duration
	durationInt1, err := strconv.Atoi(duration)
	var durationIntExec int
	if duration == "" {
		log.Fatal("Duration cannot be empty")
	}
	if err != nil {
		log.Fatal(fmt.Sprintf("Duration '%s' failed to be translated to 'int' format", duration))
	}
	durationIntExec = durationInt1 + 2
	durationInt1 = durationInt1 + 50
	duration = strconv.Itoa(durationIntExec)
	durationString := strconv.Itoa(durationInt1) + "s"
	sleepTime, err := time.ParseDuration(durationString)
	targetPods := getPodStatus(client, targetPodsp)
	podList := *targetPods
	count := len(podList)

	var workerGroup sync.WaitGroup
	for i := 0; i < count; i++ {
		workerGroup.Add(1)
		go podOperation(&workerGroup, restConfig, client, &podList[i], duration, sleepTime)
	}
	//fmt.Println("Wait for workers")
	workerGroup.Wait()
	//fmt.Println("All workers have completed")
	log.Info("All operations have been completed. EXIT now.")
}