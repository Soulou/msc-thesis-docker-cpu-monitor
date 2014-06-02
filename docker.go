package main

import (
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
