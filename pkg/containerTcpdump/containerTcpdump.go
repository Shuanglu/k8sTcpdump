package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
	"github.com/vishvananda/netns"
)

type targetContainer struct {
	Name string `json:"Name"`
}

type targetContainers struct {
	Containers []targetContainer `json:"Containers"`
	Duration   string            `json:"Duration"`
}

func tcpdump(workerGroup *sync.WaitGroup, containerID string, tContainerName string, duration string) error {
	defer workerGroup.Done()
	//tContainerJson, err := cli.ContainerInspect(ctx, container.ID)
	//tContainerJson.State.Pid
	nsHandle, _ := netns.GetFromDocker(containerID)
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

func main() {
	ctx := context.Background()
	log.SetFormatter(&log.JSONFormatter{})
	filePath := flag.String("parameter-file", "/mnt/containers.json", "path of the parameter file")
	file, err := ioutil.ReadFile(*filePath)
	hostname, _ := os.Hostname()
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to load the parameter file(%q) on node %q due to %q", *filePath, hostname, err))
	}
	containerJson := targetContainers{}
	err = json.Unmarshal([]byte(file), &containerJson)
	tContainers := containerJson.Containers
	duration := containerJson.Duration
	err = json.Unmarshal([]byte(file), &duration)
	tempDuration, _ := strconv.Atoi(duration)
	tempDuration = tempDuration + 2
	duration = strconv.Itoa(tempDuration)
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to initialize the docker client on node %q. EXIT now.", hostname))
	}
	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to list running docker containers on node %q. EXIT now.", hostname))
	}
	var workerGroup sync.WaitGroup
	for _, container := range containers {
		for _, tContainer := range tContainers {
			pattern := regexp.MustCompile(tContainer.Name)
			res := pattern.MatchString(container.Names[0])
			//res, err := regexp.MatchString(pattern.String, container.Names[0])
			if res == true {
				workerGroup.Add(1)
				go tcpdump(&workerGroup, container.ID, tContainer.Name, duration)
				//time.Sleep(time.Duration(duration) * time.Second)
			}
		}
	}
	workerGroup.Wait()
	cmd := exec.Command("touch", "/tmp/containerTcpdumpComplete")
	cmd.Run()
	cmd = exec.Command("tail", "-f /dev/null")
	cmd.Run()
}
