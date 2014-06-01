package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	docker "github.com/fsouza/go-dockerclient"
)

var startMonitoringTime time.Time

func main() {
	cgroupPath := "/cgroup"
	if os.Getenv("CGROUP_PATH") != "" {
		cgroupPath = os.Getenv("CGROUP_PATH")
	}

	dockerCPUAcctDir, err := os.Open(cgroupPath + "/cpuacct/docker")
	if err != nil {
		panic(err)
	}

	containerCPUAcctFiles, err := dockerCPUAcctDir.Readdir(0)
	if err != nil {
		panic(err)
	}

	containerCPUAcctDirs := make([]os.FileInfo, 0)
	for _, fi := range containerCPUAcctFiles {
		if fi.IsDir() {
			containerCPUAcctDirs = append(containerCPUAcctDirs, fi)
		}
	}

	var appName string
	wg := &sync.WaitGroup{}
	stop := make(chan struct{})
	startMonitoringTime = time.Now()
	valuesChans := make([]chan float64, len(containerCPUAcctDirs))
	nextChans := make([]chan struct{}, len(containerCPUAcctDirs))
	ticker := time.NewTicker(1 * time.Second)

	fmt.Printf("time")
	for i, containerCPUAcctDir := range containerCPUAcctDirs {
		wg.Add(1)
		nextChans[i] = make(chan struct{}, 1)
		appName, valuesChans[i] = monitorCPUAcct(dockerCPUAcctDir.Name()+"/"+containerCPUAcctDir.Name(), nextChans[i], stop)
		fmt.Printf(" %s", appName)
		wg.Done()
	}
	fmt.Println()

	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGINT, syscall.SIGSTOP, syscall.SIGTERM, syscall.SIGQUIT)
		<-signals
		close(stop)
	}()

	for {
		select {
		case <-stop:
			ticker.Stop()
			wg.Wait()
			os.Exit(0)
		case t := <-ticker.C:
			fmt.Printf("%v", int(t.Sub(startMonitoringTime).Seconds()))
			for i, c := range valuesChans {
				nextChans[i] <- struct{}{}
				fmt.Printf(" %2.2f%%", <-c)
			}
			fmt.Println()
		}
	}
}

func monitorCPUAcct(containerCPUAcctDir string, next chan struct{}, stopChan chan struct{}) (string, chan float64) {
	// nbCPUs := runtime.NumCPU()
	containerID := path.Base(containerCPUAcctDir)
	containerCommand := containerCommand(containerID)
	containerCPUShares := containerCPUShares(containerID)

	nbCPUs := 1
	if strings.Contains(containerCommand, "Isolation") {
		if strings.Contains(containerCommand, "-nb-cpus") {
			nbCPUs, _ = strconv.Atoi(strings.Split(containerCommand, "=")[1])
		}
	}

	values := make(chan float64, 5)

	go func() {
		previousValue := 0

		for {
			select {
			case <-stopChan:
				return
			case <-next:
				usage, err := ioutil.ReadFile(containerCPUAcctDir + "/cpuacct.usage")
				if err != nil {
					fmt.Println("End of monitoring for", containerCPUAcctDir)
					return
				}
				usageInt, err := strconv.Atoi(strings.TrimRight(string(usage), "\n"))
				if err != nil {
					panic(err)
				}
				values <- (float64(usageInt-previousValue) / float64(time.Second) * 100.0)
				previousValue = usageInt
			}
		}
	}()

	return fmt.Sprintf("cpu-%v-%v", nbCPUs, containerCPUShares), values
}

func containerCommand(id string) string {
	client, err := docker.NewClient("unix:///docker.sock")
	if err != nil {
		panic(err)
	}
	container, err := client.InspectContainer(id)
	if err != nil {
		panic(err)
	}
	if len(container.Config.Cmd) == 0 {
		return strings.Join(container.Config.Entrypoint, " ")
	} else {
		return strings.Join(container.Config.Entrypoint, " ") + " " + strings.Join(container.Config.Cmd, " ")
	}
}

func containerCPUShares(id string) int64 {
	client, err := docker.NewClient("unix:///docker.sock")
	if err != nil {
		panic(err)
	}
	container, err := client.InspectContainer(id)
	if err != nil {
		panic(err)
	}
	return container.Config.CpuShares
}
