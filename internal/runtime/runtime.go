package runtime

// Mode identifies a runtime backend.
type Mode string

const (
	ModeLocal      Mode = "local"
	ModeDocker     Mode = "docker"
	ModeClamshell  Mode = "clamshell"
	ModeKubernetes Mode = "kubernetes"
)

// Runtime captures the active backend selection for a session launch.
// The higher-level CLI still owns concrete lifecycle plumbing, but launch
// selection and capability checks now flow through this runtime layer.
type Runtime interface {
	Mode() Mode
	Containerized() bool
	SupportsDynamicAgents() bool
}

type LocalRuntime struct{}

func (LocalRuntime) Mode() Mode                  { return ModeLocal }
func (LocalRuntime) Containerized() bool         { return false }
func (LocalRuntime) SupportsDynamicAgents() bool { return true }

type DockerRuntime struct{}

func (DockerRuntime) Mode() Mode                  { return ModeDocker }
func (DockerRuntime) Containerized() bool         { return true }
func (DockerRuntime) SupportsDynamicAgents() bool { return false }

type ClamshellRuntime struct{}

func (ClamshellRuntime) Mode() Mode                  { return ModeClamshell }
func (ClamshellRuntime) Containerized() bool         { return true }
func (ClamshellRuntime) SupportsDynamicAgents() bool { return true }

type KubernetesRuntime struct{}

func (KubernetesRuntime) Mode() Mode                  { return ModeKubernetes }
func (KubernetesRuntime) Containerized() bool         { return true }
func (KubernetesRuntime) SupportsDynamicAgents() bool { return false }

// Select chooses the current runtime backend from the launch mode flags.
func Select(useDocker, useClamshell bool) Runtime {
	if useClamshell {
		return ClamshellRuntime{}
	}
	if useDocker {
		return DockerRuntime{}
	}
	return LocalRuntime{}
}
