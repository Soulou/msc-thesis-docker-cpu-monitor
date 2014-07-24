package main

import (
	"strings"

	docker "github.com/fsouza/go-dockerclient"
)

var (
	dockerHost = "unix:///var/run/docker.sock"
)

func DockerClient() *docker.Client {
	client, err := docker.NewClient(dockerHost)
	if err != nil {
		panic(err)
	}
	return client
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
