##@ Code Generation

CONTROLLER_GEN ?= $(shell which controller-gen 2>/dev/null)

.PHONY: generate
generate: ## Generate deepcopy methods and CRD manifests.
	@echo "====> $@"
	$(CONTROLLER_GEN) object paths="./api/..."
