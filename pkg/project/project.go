package project

var (
	description    = "Kubernetes operator for dynamic management of Klaus AI agent instances."
	gitSHA         = "n/a"
	name           = "klaus-operator"
	source         = "https://github.com/giantswarm/klaus-operator"
	version        = "0.1.0"
	buildTimestamp = "n/a"
)

func Description() string {
	return description
}

func GitSHA() string {
	return gitSHA
}

func Name() string {
	return name
}

func Source() string {
	return source
}

func Version() string {
	return version
}

func BuildTimestamp() string {
	return buildTimestamp
}
