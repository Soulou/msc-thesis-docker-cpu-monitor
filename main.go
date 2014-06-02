package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	dockerCgroupPath  = "/sys/fs/cgroup/cpuacct/docker"
	systemdCgroupPath = "/sys/fs/cgroup/cpuacct/system.slice"
)

var (
	isUsingSystemd          bool
	systemdDirnameRegexp    = regexp.MustCompile("docker-[a-f0-9]+.scope")
	dockerContainerIDRegexp = regexp.MustCompile("[a-f0-9]{64}")
	startMonitoringTime     time.Time
)

func main() {
	cgroupPath := dockerCgroupPath

	flag.StringVar(&dockerHost, "docker-host", dockerHost, "Docker host (unix socket or tcp endpoint)")
	customCgroupPath := flag.String("cgroup-path", dockerCgroupPath, "Path to the cpuacct cgroup subgroup directory")
	flag.BoolVar(&isUsingSystemd, "use-systemd", false, "Adapt path to systemd scheme to access cpuacct files")
	flag.Parse()

	if isUsingSystemd {
		cgroupPath = systemdCgroupPath
	}

	if *customCgroupPath != dockerCgroupPath {
		cgroupPath = *customCgroupPath
	}

	_, err := os.Stat(cgroupPath)
	if os.IsNotExist(err) {
		log.Fatalln("Cgroup directory", cgroupPath, "doesn't exist")
	}

	dockerCPUAcctDir, err := os.Open(cgroupPath)
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
			// If systemd is used we just want the containers
			if isUsingSystemd && !systemdDirnameRegexp.MatchString(fi.Name()) {
				continue
			}
			containerCPUAcctDirs = append(containerCPUAcctDirs, fi)
		}
	}

	var appName string
	wg := &sync.WaitGroup{}
	stop := make(chan struct{})
	startMonitoringTime = time.Now()
	containerMonitors := make([]*ContainerMonitor, len(containerCPUAcctDirs))
	nextChans := make([]chan struct{}, len(containerCPUAcctDirs))
	ticker := time.NewTicker(1 * time.Second)

	fmt.Printf("time")
	for i, containerCPUAcctDir := range containerCPUAcctDirs {
		wg.Add(1)
		nextChans[i] = make(chan struct{}, 1)
		appName, containerMonitors[i] = monitorCPUAcct(dockerCPUAcctDir.Name()+"/"+containerCPUAcctDir.Name(), nextChans[i], stop)
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
			for i, cm := range containerMonitors {
				if !cm.closed {
					nextChans[i] <- struct{}{}
					fmt.Printf(" %2.2f%%", <-cm.valuesChan)
				} else {
					fmt.Printf("  end ")
				}
			}
			fmt.Println()
		}
	}
}

type ContainerMonitor struct {
	valuesChan chan float64
	closed     bool
}

func monitorCPUAcct(containerCPUAcctDir string, next chan struct{}, stopChan chan struct{}) (string, *ContainerMonitor) {
	// nbCPUs := runtime.NumCPU()
	var containerID string
	if isUsingSystemd {
		containerID = dockerContainerIDRegexp.FindString(containerCPUAcctDir)
	} else {
		containerID = path.Base(containerCPUAcctDir)
	}
	containerCommand := containerCommand(containerID)
	containerCPUShares := containerCPUShares(containerID)

	nbCPUs := 1
	if strings.Contains(containerCommand, "Isolation") {
		if strings.Contains(containerCommand, "-nb-cpus") {
			nbCPUs, _ = strconv.Atoi(strings.Split(containerCommand, "=")[1])
		}
	}

	cm := &ContainerMonitor{
		valuesChan: make(chan float64, 5),
	}

	go func() {
		defer func() {
			cm.closed = true
			close(cm.valuesChan)
		}()

		previousValue := 0

		for {
			select {
			case <-stopChan:
				return
			case <-next:
				usage, err := ioutil.ReadFile(containerCPUAcctDir + "/cpuacct.usage")
				if err != nil {
					return
				}
				usageInt, err := strconv.Atoi(strings.TrimRight(string(usage), "\n"))
				if err != nil {
					panic(err)
				}
				cm.valuesChan <- (float64(usageInt-previousValue) / float64(time.Second) * 100.0)
				previousValue = usageInt
			}
		}
	}()

	return fmt.Sprintf("cpu-%v-%v", nbCPUs, containerCPUShares), cm
}

func containerCommand(id string) string {
	client := DockerClient()
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
	client := DockerClient()
	container, err := client.InspectContainer(id)
	if err != nil {
		panic(err)
	}
	return container.Config.CpuShares
}
