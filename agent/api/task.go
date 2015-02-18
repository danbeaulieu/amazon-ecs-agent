package api

import (
	"errors"
	"strconv"
	"strings"

	"github.com/fsouza/go-dockerclient"
)

func (task *Task) ContainersByName() map[string]*Container {
	task.containersByNameLock.Lock()
	defer task.containersByNameLock.Unlock()

	if task.containersByName != nil {
		return task.containersByName
	}
	task.containersByName = make(map[string]*Container)
	for _, container := range task.Containers {
		task.containersByName[container.Name] = container
	}
	return task.containersByName
}

func (task *Task) ContainerByName(name string) (*Container, bool) {
	container, ok := task.ContainersByName()[name]
	return container, ok
}

// InferContainerDesiredStatus ensures that all container's desired statuses are
// compatible with whatever status the task desires to be at or is at.
// This is used both to initialize container statuses of new tasks and to force
// auxilery containers into terminal states (e.g. the essential containers died
// already)
func (task *Task) InferContainerDesiredStatus() {
	for _, c := range task.Containers {
		c.DesiredStatus = task.maxStatus().ContainerStatus()
	}
}

func (task *Task) maxStatus() *TaskStatus {
	if task.KnownStatus > task.DesiredStatus {
		return &task.KnownStatus
	}
	return &task.DesiredStatus
}

// Overridden returns a copy of the task with all container's overridden and
// itself overridden as well
func (task *Task) Overridden() *Task {
	result := *task
	// Task has no overrides currently, just do the containers

	// Shallow copy, take care of the deeper bits too
	result.containersByNameLock.Lock()
	result.containersByName = make(map[string]*Container)
	result.containersByNameLock.Unlock()

	result.Containers = make([]*Container, len(result.Containers))
	for i, cont := range task.Containers {
		result.Containers[i] = cont.Overridden()
	}
	return &result
}

func (task *Task) DockerHostConfig(container *Container, dockerContainerMap map[string]*DockerContainer) (*docker.HostConfig, error) {
	return task.Overridden().dockerHostConfig(container.Overridden(), dockerContainerMap)
}

func (task *Task) dockerHostConfig(container *Container, dockerContainerMap map[string]*DockerContainer) (*docker.HostConfig, error) {
	dockerLinkArr := make([]string, 0, len(container.Links))
	for _, link := range container.Links {
		linkParts := strings.Split(link, ":")
		if len(linkParts) > 2 {
			return nil, errors.New("Invalid link format")
		}
		linkName := linkParts[0]
		var linkAlias string

		if len(linkParts) == 2 {
			linkAlias = linkParts[1]
		} else {
			log.Warn("Warning, link with linkalias", "linkName", linkName, "task", task, "container", container)
			linkAlias = linkName
		}

		targetContainer, ok := dockerContainerMap[linkName]
		if !ok {
			return nil, errors.New("Link target not available: " + linkName)
		}
		fixedLink := targetContainer.DockerName + ":" + linkAlias
		dockerLinkArr = append(dockerLinkArr, fixedLink)
	}

	dockerPortMap := make(map[docker.Port][]docker.PortBinding)

	for _, portBinding := range container.Ports {
		dockerPort := docker.Port(strconv.Itoa(int(portBinding.ContainerPort)) + "/tcp")
		currentMappings, existing := dockerPortMap[dockerPort]
		if existing {
			dockerPortMap[dockerPort] = append(currentMappings, docker.PortBinding{HostIP: "0.0.0.0", HostPort: strconv.Itoa(int(portBinding.HostPort))})
		} else {
			dockerPortMap[dockerPort] = []docker.PortBinding{docker.PortBinding{HostIP: "0.0.0.0", HostPort: strconv.Itoa(int(portBinding.HostPort))}}
		}
	}

	hostConfig := &docker.HostConfig{Links: dockerLinkArr, Binds: []string{"/tmp:/tmp:rw"}, PortBindings: dockerPortMap}
	return hostConfig, nil
}
