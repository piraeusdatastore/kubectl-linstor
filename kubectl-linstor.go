package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
)

// Expand argument of form "<resource>:..." to LINSTOR resources names.
//
// Currently supported resource names are "pod" and "pvc".
func expandSpecialArgToLinstorResourceNames(ctx context.Context, arg string) []string {
	parts := strings.SplitN(arg, ":", 2)
	if len(parts) != 2 {
		return []string{arg}
	}

	switch strings.ToLower(parts[0]) {
	case "pvc":
		pvname, err := replacePVCWithPVArg(ctx, parts[1])
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "could not convert PVC to PV name, continue with unexpanded arg '%s': %s\n", arg, err.Error())
			return []string{arg}
		}

		_, _ = fmt.Fprintf(os.Stderr, "%s -> %s\n", arg, pvname)
		return []string{pvname}
	case "pod":
		pvNames, pvcNames, err := replacePodWithLinstorPV(ctx, parts[1])
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "could not convert pod to PV names, continue with unexpanded arg '%s': %s\n", arg, err.Error())
			return []string{arg}
		}

		pvcMapping := make([]string, len(pvNames))
		for i := range pvNames {
			pvcMapping[i] = fmt.Sprintf("[%s -> %s]", pvcNames[i], pvNames[i])
		}

		_, _ = fmt.Fprintf(os.Stderr, "%s -> %s\n", arg, strings.Join(pvcMapping, " "))
		return pvNames
	default:
		return []string{arg}
	}
}

// Converts arguments of form "pod:[namespace/]podname" to LINSTOR resource names.
//
// The Pod is converted to LINSTOR resources by resolving all persistent volume claims to PVs (see replacePVCWithPVArg).
// A single "pod:" argument can expand to multiple resource names, if the pod has more than one PVCs.
func replacePodWithLinstorPV(ctx context.Context, arg string) ([]string, []string, error) {
	namespace, name, namespaceArgs := maybeNamespacedArgToKubectlArgs(arg)
	kubectlArgs := []string{"get", "pods", name, "--output", "jsonpath={.spec.volumes[*].persistentVolumeClaim.claimName}"}
	kubectlArgs = append(kubectlArgs, namespaceArgs...)
	pvcOut, err := exec.CommandContext(ctx, "kubectl", kubectlArgs...).Output()
	if err != nil {
		if len(namespaceArgs) == 0 {
			return nil, nil, fmt.Errorf("maybe missing namespace: pod:<namespace>/%s", name)
		}

		return nil, nil, fmt.Errorf("could not convert Pod to PVCs: %s", err.(*exec.ExitError).Stderr)
	}

	pvcNames := strings.Fields(string(pvcOut))

	pvNames := make([]string, len(pvcNames))
	for i, pvc := range pvcNames {
		if namespace != "" {
			pvc = fmt.Sprintf("%s/%s", namespace, pvc)
		}
		pv, err := replacePVCWithPVArg(ctx, pvc)
		if err != nil {
			return nil, nil, fmt.Errorf("could not convert Pod's PVC to PV: %w", err)
		}
		pvNames[i] = pv
	}

	return pvNames, pvcNames, nil
}

// Converts arguments of form "pvc:[namespace/]pvcname" to LINSTOR resource names.
//
// LINSTOR resource names are based on the PV names created, so it really converts to the name of the PV bound to
// the PVC. If the argument does not match the "pvc:..." schema, it is returned unchanged.
func replacePVCWithPVArg(ctx context.Context, arg string) (string, error) {
	_, name, namespaceArgs := maybeNamespacedArgToKubectlArgs(arg)
	kubectlArgs := []string{"get", "persistentvolumeclaims", name, "--output", "jsonpath={.spec.volumeName}"}
	kubectlArgs = append(kubectlArgs, namespaceArgs...)
	pvname, err := exec.CommandContext(ctx, "kubectl", kubectlArgs...).Output()
	if err != nil {
		if len(namespaceArgs) == 0 {
			return "", fmt.Errorf("maybe missing namespace: pvc:<namespace>/%s", name)
		}

		return "", fmt.Errorf("could not convert PVC to PV name: %s", err.(*exec.ExitError).Stderr)
	}

	pvname = bytes.TrimSpace(pvname)
	if len(pvname) == 0 {
		return "", fmt.Errorf("could not find volume name for PVC '%s'", arg)
	}

	return string(bytes.TrimSpace(pvname)), nil
}

func maybeNamespacedArgToKubectlArgs(arg string) (string, string, []string) {
	parts := strings.SplitN(arg, "/", 2)
	switch len(parts) {
	case 1:
		return "", parts[0], nil
	case 2:
		return parts[0], parts[1], []string{"--namespace", parts[0]}
	default:
		log.Fatalf("wtf: strings.SplitN(arg, \"/\", 2) returned %d parts", len(parts))
	}
	return "", "", nil
}

func getControllerDeploymentNamespacedName(ctx context.Context) (string, string, error) {
	out, err := exec.CommandContext(ctx, "kubectl", "api-resources", "-oname").Output()
	if err != nil {
		log.Fatalf("failed to fetch Cluster APIs: %v", err)
	}

	apiResources := strings.Fields(string(out))

	for _, apiResource := range apiResources {
		switch strings.SplitN(apiResource, ".", 2)[0] {
		case "linstorclusters":
			out, err := exec.CommandContext(ctx, "kubectl", "get", "--all-namespaces", "deployments", "--output", "jsonpath={.items[*].metadata.namespace},{.items[*].metadata.name}", "--selector", "app.kubernetes.io/component=linstor-controller").Output()
			if err != nil {
				return "", "", fmt.Errorf("failed to fetch LINSTOR controller resource: %w", err)
			}

			controllerDeployments := strings.Fields(string(out))
			if len(controllerDeployments) == 0 {
				log.Fatal("could not find a LINSTOR Controller Deployment resource")
			}

			if len(controllerDeployments) > 1 {
				log.Fatalf("found more than one LINSTOR Controller Deployment resource: %v", controllerDeployments)
			}

			parts := strings.SplitN(controllerDeployments[0], ",", 2)
			return parts[0], parts[1], nil
		case "linstorcontrollers":
			out, err := exec.CommandContext(ctx, "kubectl", "get", "--all-namespaces", "linstorcontrollers", "--output", "jsonpath={.items[*].metadata.namespace},{.items[*].metadata.name}").Output()
			if err != nil {
				return "", "", fmt.Errorf("failed to fetch LINSTOR controller resource: %w", err)
			}

			controllerResources := strings.Fields(string(out))
			if len(controllerResources) == 0 {
				log.Fatal("could not find a LinstorController resource")
			}

			if len(controllerResources) > 1 {
				log.Fatalf("found more than one LinstorController resource: %v", controllerResources)
			}

			parts := strings.SplitN(controllerResources[0], ",", 2)
			return parts[0], parts[1] + "-controller", nil
		}
	}

	return "", "", fmt.Errorf("could not find a managed LINSTOR Controller resource")
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	namespace, name, err := getControllerDeploymentNamespacedName(ctx)
	if err != nil {
		log.Fatalf("%s", err)
	}

	toExecArgs := []string{"exec", "--namespace", namespace, "--stdin"}
	if fileInfo, _ := os.Stdout.Stat(); (fileInfo.Mode() & os.ModeCharDevice) != 0 {
		// Enable interactive mode in case the plugin is running in a tty.
		toExecArgs = append(toExecArgs, "--tty")
	}
	deploymentRef := fmt.Sprintf("deployment/%s", name)

	toExecArgs = append(toExecArgs, deploymentRef, "--", "linstor")
	for _, arg := range os.Args[1:] {
		toExecArgs = append(toExecArgs, expandSpecialArgToLinstorResourceNames(ctx, arg)...)
	}

	cmd := exec.CommandContext(ctx, "kubectl", toExecArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
	os.Exit(cmd.ProcessState.ExitCode())
}
