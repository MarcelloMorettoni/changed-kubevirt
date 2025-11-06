package main

import (
	stdjson "encoding/json"
	"log"
	"strings"
)

var requiredCapabilities = []string{
	"DAC_OVERRIDE",
	"NET_ADMIN",
	"SYS_RAWIO",
}

type podSecurityMutator struct{}

func (p *podSecurityMutator) Handle(request *AdmissionRequest) *AdmissionResponse {
	if request == nil {
		return &AdmissionResponse{Allowed: true}
	}

	if !strings.EqualFold(request.Kind.Kind, "Pod") {
		return &AdmissionResponse{Allowed: true}
	}

	var pod map[string]interface{}
	if err := stdjson.Unmarshal(request.Object, &pod); err != nil {
		log.Printf("failed to unmarshal pod: %v", err)
		return &AdmissionResponse{
			Allowed: false,
			Status:  &Status{Message: err.Error()},
		}
	}

	if !usesMacvtap(pod) {
		return &AdmissionResponse{Allowed: true}
	}

	spec, _ := pod["spec"].(map[string]interface{})
	if spec == nil {
		return &AdmissionResponse{Allowed: true}
	}

	patchSpec := map[string]interface{}{}

	if containers, ok := spec["initContainers"].([]interface{}); ok {
		if mutated, changed := mutateContainers(containers); changed {
			patchSpec["initContainers"] = mutated
		}
	}

	if containers, ok := spec["containers"].([]interface{}); ok {
		if mutated, changed := mutateContainers(containers); changed {
			patchSpec["containers"] = mutated
		}
	}

	if len(patchSpec) == 0 {
		return &AdmissionResponse{Allowed: true}
	}

	patch := map[string]interface{}{
		"spec": patchSpec,
	}

	patchBytes, err := stdjson.Marshal(patch)
	if err != nil {
		log.Printf("failed to marshal patch: %v", err)
		return &AdmissionResponse{Allowed: false, Status: &Status{Message: err.Error()}}
	}

	patchType := PatchTypeJSONMergePatch
	return &AdmissionResponse{
		Allowed:   true,
		Patch:     patchBytes,
		PatchType: &patchType,
	}
}

func mutateContainers(containers []interface{}) ([]interface{}, bool) {
	mutated := make([]interface{}, len(containers))
	anyChanged := false

	for i, c := range containers {
		container, ok := c.(map[string]interface{})
		if !ok {
			mutated[i] = c
			continue
		}

		containerCopy := deepCopyMap(container)
		containerChanged := false

		sc, _ := containerCopy["securityContext"].(map[string]interface{})
		if sc == nil {
			sc = map[string]interface{}{}
		}

		if val, ok := sc["privileged"].(bool); !ok || !val {
			sc["privileged"] = true
			containerChanged = true
		}

		caps, _ := sc["capabilities"].(map[string]interface{})
		if caps == nil {
			caps = map[string]interface{}{}
		}

		addSlice, _ := caps["add"].([]interface{})
		have := map[string]struct{}{}
		for _, entry := range addSlice {
			if s, ok := entry.(string); ok {
				have[s] = struct{}{}
			}
		}

		initialLen := len(addSlice)
		for _, capability := range requiredCapabilities {
			if _, ok := have[capability]; !ok {
				addSlice = append(addSlice, capability)
				have[capability] = struct{}{}
			}
		}

		if len(addSlice) != initialLen {
			caps["add"] = addSlice
			containerChanged = true
		}

		sc["capabilities"] = caps
		containerCopy["securityContext"] = sc
		mutated[i] = containerCopy

		if containerChanged {
			anyChanged = true
		}
	}

	return mutated, anyChanged
}

func usesMacvtap(pod map[string]interface{}) bool {
	metadata, _ := pod["metadata"].(map[string]interface{})
	if metadata != nil {
		if annotations, ok := metadata["annotations"].(map[string]interface{}); ok {
			for key, value := range annotations {
				keyLower := strings.ToLower(key)
				if strings.Contains(keyLower, "k8s.v1.cni.cncf.io/resource") || strings.Contains(keyLower, "k8s.v1.cni.cncf.io/networks") {
					if str, ok := value.(string); ok && strings.Contains(strings.ToLower(str), "macvtap") {
						return true
					}
				}
			}
		}
	}

	spec, _ := pod["spec"].(map[string]interface{})
	if spec == nil {
		return false
	}

	containersFields := []string{"initContainers", "containers"}
	for _, field := range containersFields {
		containerList, _ := spec[field].([]interface{})
		for _, c := range containerList {
			if container, ok := c.(map[string]interface{}); ok {
				if resources, ok := container["resources"].(map[string]interface{}); ok {
					if resourcesUseMacvtap(resources) {
						return true
					}
				}
			}
		}
	}

	return false
}

func resourcesUseMacvtap(resources map[string]interface{}) bool {
	for _, key := range []string{"limits", "requests"} {
		if resMap, ok := resources[key].(map[string]interface{}); ok {
			for name := range resMap {
				if isMacvtapResource(name) {
					return true
				}
			}
		}
	}
	return false
}

func isMacvtapResource(resource string) bool {
	resource = strings.ToLower(resource)
	return strings.HasPrefix(resource, "macvtap.network.kubevirt.io/") || strings.Contains(resource, "macvtap")
}

func deepCopyMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return deepCopyMap(val)
	case []interface{}:
		copied := make([]interface{}, len(val))
		for i := range val {
			copied[i] = deepCopyValue(val[i])
		}
		return copied
	default:
		return val
	}
}
