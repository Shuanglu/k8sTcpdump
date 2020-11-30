package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	//"syscall"

	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
	"github.com/vishvananda/netns"
)

const (
	CLONE_NEWUTS  = 0x04000000   /* New utsname group? */
	CLONE_NEWIPC  = 0x08000000   /* New ipcs */
	CLONE_NEWUSER = 0x10000000   /* New user namespace */
	CLONE_NEWPID  = 0x20000000   /* New pid namespace */
	CLONE_NEWNET  = 0x40000000   /* New network namespace */
	CLONE_IO      = 0x80000000   /* Get io context */
	CLONE_NEWNS   = 0x20000      /* New mount namespace */
	bindMountPath = "/run/netns" /* Bind mount path for named netns */
)

type targetContainer struct {
	Name string `json:"Name"`
}

type targetContainers struct {
	Containers []targetContainer `json:"Containers"`
	Duration   string            `json:"Duration"`
}

func tcpdump(workerGroup *sync.WaitGroup, containerPid int, tContainerName string, duration string) error {
	defer workerGroup.Done()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	//tContainerJson, err := cli.ContainerInspect(ctx, container.ID)
	//tContainerJson.State.Pid
	nsHandle, _ := netns.GetFromPid(containerPid)
	netns.Set(nsHandle)
	//err := os.Remove("/tmp/" + tContainerName + ".cap")
	cmd := exec.Command("/bin/bash", "-c", "timeout "+duration+" tcpdump -i any -w /tmp/"+tContainerName+".cap")
	err := cmd.Start()
	log.Info("Command starts")
	err = cmd.Wait()
	if err != nil {
		if fmt.Sprintf("%s", err) == "exit status 124" {
			log.Info(fmt.Sprintf("tcpdump command for the container %q completed successfully", tContainerName))
		} else {
			log.Info(fmt.Sprintf("tcpdump command for the container %q didn't complete successfully due to %q", tContainerName, err))
		}

	}
	return nil
}

/*func getContainerPIDs() (map[string]int, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	//originPid := os.Getpid()
	cmd := exec.Command("/bin/bash", "-c", "nsenter -t 1 -m -u")
	//err := cmd.Run()
	//if err != nil {
	//	log.Info(fmt.Sprintf("Failed to switch to the init namespace due to %q", err))
	//} else {
	//	log.Info("Succesfully switch to the init namespace")
	//}
	//originNS, _ := netns.GetFromPath("/proc/" + strconv.Itoa(originPid) + "/ns/pid")
	//defer originNS.Close()
	//nsPIDFD, err := unix.Open("/proc/1/ns/pid", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	//nsUTSFD, err := unix.Open("/proc/1/ns/uts", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	//nsMNTFD, err := unix.Open("/proc/1/ns/mnt", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	//nsNETFD, err := unix.Open("/proc/1/ns/net", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	//nsHandle, err := netns.GetFromPath("/proc/1/ns/pid")
	//log.Info(strconv.Itoa(int(nsUTSFD)))
	//err = unix.Setns(nsPIDFD, CLONE_NEWPID)
	//if err != nil {
	//	log.Fatal(fmt.Sprintf("Failed to set the 'PID' namespace due to %q", err))
	//}
	//err = unix.Setns(nsUTSFD, CLONE_NEWUTS)
	//if err != nil {
	//	log.Fatal(fmt.Sprintf("Failed to set the 'UTS' namespace due to %q", err))
	//}
	//err = unix.Setns(nsNETFD, CLONE_NEWNET)
	//hostname, _ := os.Hostname()
	//if err != nil {
	//	log.Fatal(fmt.Sprintf("Failed to set the 'net' namespace due to %q", err))
	//}
	//err = unix.Setns(nsMNTFD, CLONE_NEWNS)
	//hostname, _ := os.Hostname()
	//if err != nil {
	//	log.Info(hostname)
	//	log.Fatal(fmt.Sprintf("Failed to set the 'mount' namespace due to %q", err))
	//}

	//log.Info(fmt.Sprintf(strconv.Itoa(unix.Getpid())))

	//if err != nil {
	//	log.Fatal("Failed to enter the namespace of the pid 1 on node %q", hostname)
	//}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to initialize the docker client on node %q. EXIT now.", hostname))
	}
	containers, err := cli.ContainerList(context.TODO(), types.ContainerListOptions{})
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to list running docker containers on node %q due to error %q. EXIT now.", hostname, err))
	}
	containerPids := make(map[string]int)
	for _, container := range containers {
		containerInspect, err := cli.ContainerInspect(context.TODO(), container.ID)
		if err != nil {
			log.Info(fmt.Sprintf("Failed to get detail of the container %q", container.ID))
		}
		containerPids[containerInspect.Name] = containerInspect.State.Pid
		//log.Info()
	}
	//unix.Setns(int(originNS), CLONE_NEWPID)
	//unix.Setns(int(originNS), CLONE_NEWNS)
	//cmd = exec.Command("/bin/bash", "-c", "exit")
	//cmd.Run()
	return containerPids, nil
}*/

func getgetContainerPID(tContainerName string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	//cmd := exec.Command("/bin/bash", "-c", "nsenter -t 1 -m docker inspect $(docker ps |grep '"+tContainerName+"'|awk '{print $1}')|grep 'Pid'")
	//cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWPID |
	//	syscall.CLONE_NEWNS,
	//containerName := fmt.Sprintf("\"%s\"", tContainerName)
	command := "nsenter --target 1 --mount -- /bin/bash -c \"docker ps |grep '" + tContainerName + "'\""
	cmd := exec.Command("/bin/bash", "-c", command)
	output, err := cmd.CombinedOutput()
	//log.Info(command)
	if err != nil {
		log.Warn(fmt.Sprintf("Command execution returned error %q", err))
	}
	tContainerInfo := string(output)
	tContainerID := strings.Split(tContainerInfo, " ")[0]
	//log.Info(tContainerID)
	//| awk "{print $1}")|grep "Pid"
	// + tContainerName + '" | awk "{print $1}")|grep "Pid"'
	command = "nsenter --target 1 --mount -- /bin/bash -c 'docker inspect " + tContainerID + "'"
	//log.Info(command)
	cmd = exec.Command("/bin/bash", "-c", command)
	output, err = cmd.CombinedOutput()
	//log.Info(string(output))
	pattern := regexp.MustCompile("\"Pid\": ([0-9]+)")
	tempContainerPid := pattern.FindStringSubmatch(string(output))[0]
	//log.Info(tempContainerPid)
	pattern = regexp.MustCompile("([0-9]+)")
	containerPid := pattern.FindStringSubmatch(tempContainerPid)[0]
	//log.Info(containerPid)
	if err != nil {
		log.Warn(fmt.Sprintf("Command execution returned error %q", err))
	}
	return containerPid, err
}

func main() {
	//ctx := context.Background()

	log.SetFormatter(&log.JSONFormatter{})
	filePath := flag.String("parameEter-file", "/mnt/containerTcpdump/containers.json", "path of the parameter file")
	file, err := ioutil.ReadFile(*filePath)
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to load the parameter file(%q) on node due to %q", *filePath, err))
	}
	containerJson := targetContainers{}
	err = json.Unmarshal([]byte(file), &containerJson)
	tContainers := containerJson.Containers
	duration := containerJson.Duration
	err = json.Unmarshal([]byte(file), &duration)
	tempDuration, _ := strconv.Atoi(duration)
	tempDuration = tempDuration + 2
	duration = strconv.Itoa(tempDuration)

	//containerPids := make(map[string]int)
	//containerPids, err = getContainerPIDs()
	var workerGroup sync.WaitGroup
	for _, tContainer := range tContainers {
		tContainerPidStr, err := getgetContainerPID(tContainer.Name)
		log.Info(fmt.Sprintf("PID of the container %q is %q", tContainer.Name, tContainerPidStr))
		if err == nil {
			workerGroup.Add(1)
			tContainerPid, _ := strconv.Atoi(tContainerPidStr)
			go tcpdump(&workerGroup, tContainerPid, tContainer.Name, duration)
		} else {
			log.Warn(fmt.Sprintf("Failed to get the PID for container %q due to %q", tContainer.Name, err))
		}

		/*pattern := regexp.MustCompile(tContainer.Name)
		for containerName, containerPid := range containerPids {
			if pattern.MatchString(containerName) == true {
				workerGroup.Add(1)
				go tcpdump(&workerGroup, containerPid, tContainer.Name, duration)
			}
		}*/

	}
	workerGroup.Wait()
	cmd := exec.Command("touch", "/tmp/containerTcpdumpComplete")
	cmd.Run()
}
