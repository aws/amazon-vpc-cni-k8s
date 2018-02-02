package api

// TransitionDependencySet contains dependencies that impact transitions of
// containers.
type TransitionDependencySet struct {
	// ContainerDependencies is the set of containers on which a transition is
	// dependent.
	ContainerDependencies []ContainerDependency `json:"ContainerDependencies"`
}

// ContainerDependency defines the relationship between a dependent container
// and its dependency.
type ContainerDependency struct {
	// ContainerName defines the container on which a transition depends
	ContainerName string `json:"ContainerName"`
	// SatisfiedStatus defines the status that satisfies the dependency
	SatisfiedStatus ContainerStatus `json:"SatisfiedStatus"`
	// DependentStatus defines the status that cannot be reached until the
	// resource satisfies the dependency
	DependentStatus ContainerStatus `json:"DependentStatus"`
}
