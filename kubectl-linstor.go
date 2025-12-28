package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
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

func getControllerPodNamespacedName(ctx context.Context) (string, string, error) {
	out, err := exec.CommandContext(ctx, "kubectl", "api-resources", "-oname").Output()
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch Cluster APIs: %v", err)
	}

	apiResources := strings.Fields(string(out))

	for _, apiResource := range apiResources {
		switch strings.SplitN(apiResource, ".", 2)[0] {
		case "linstorclusters":
			out, err := exec.CommandContext(ctx, "kubectl", "get", "--all-namespaces", "pods", "--output", "jsonpath={range .items[*]}{.metadata.namespace},{.metadata.name}{end}", "--selector", "app.kubernetes.io/component=linstor-controller").Output()
			if err != nil {
				return "", "", fmt.Errorf("failed to fetch LINSTOR controller pods: %w", err)
			}

			controllerPods := strings.Fields(string(out))
			if len(controllerPods) == 0 {
				return "", "", fmt.Errorf("could not find a LINSTOR Controller Pod")
			}

			parts := strings.SplitN(controllerPods[0], ",", 2)
			return parts[0], parts[1], nil
		case "linstorcontrollers":
			out, err := exec.CommandContext(ctx, "kubectl", "get", "--all-namespaces", "linstorcontrollers", "--output", "jsonpath={range .items[*]}{.metadata.namespace},{.metadata.name}{end}").Output()
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
			out, err = exec.CommandContext(ctx, "kubectl", "get", "--namespace", parts[0], "pods", "--selector", "app.kubernetes.io/instance="+parts[1], "--output", "jsonpath={range .items[*]}{.metadata.name}{end}").Output()
			if err != nil {
				return "", "", fmt.Errorf("failed to fetch LINSTOR controller pods: %w", err)
			}

			controllerPods := strings.Fields(string(out))
			if len(controllerPods) == 0 {
				return "", "", fmt.Errorf("could not find a LINSTOR Controller Pod")
			}

			return parts[0], controllerPods[0], nil
		}
	}

	return "", "", fmt.Errorf("could not find a managed LINSTOR Controller resource")
}

func isVersion(args ...string) bool {
	if len(args) != 1 {
		return false
	}

	return args[0] == "version"
}

func isSosReportDownload(args ...string) bool {
	if len(args) < 2 {
		return false
	}

	isSosReport := args[0] == "sos" || args[0] == "sos-report"
	isDownload := args[1] == "dl" || args[1] == "download"
	return isSosReport && isDownload
}

func doSosReportDownload(ctx context.Context, namespace, pod string, kubectlArgs []string, args ...string) {
	flags := pflag.NewFlagSet("download", pflag.ContinueOnError)
	help := flags.BoolP("help", "h", false, "")
	since := flags.StringP("since", "s", "", "Create sos-report with logs since n days. e.g. \"3days\"")
	nodes := flags.StringArrayP("nodes", "n", nil, "Only include the given nodes in the sos-report")
	resources := flags.StringArrayP("resources", "r", nil, "Only include nodes that have the given resources deployed in the sos-report")
	exclude := flags.StringArrayP("exclude-nodes", "e", nil, "Do not include the given nodes in the sos-report")
	noController := flags.Bool("no-controller", false, "Do not include the controller in the sos-report")
	err := flags.Parse(args[2:])
	if err != nil {
		log.Fatalf("failed to parse flags: %s", err)
	}

	if *help {
		fmt.Printf(flags.FlagUsages())
		return
	}

	if len(flags.Args()) > 1 {
		fmt.Printf(flags.FlagUsages())
		_, _ = fmt.Fprintln(os.Stderr, "Expected at most one path argument")
		os.Exit(1)
	}

	createCmd := append(kubectlArgs, "linstor", "-m", "--output-version", "v1", "sos-report", "create")
	if *since != "" {
		createCmd = append(createCmd, "--since", *since)
	}
	if len(*nodes) > 0 {
		createCmd = append(createCmd, append([]string{"--nodes"}, *nodes...)...)
	}
	if len(*resources) > 0 {
		createCmd = append(createCmd, append([]string{"--resources"}, *resources...)...)
	}
	if len(*exclude) > 0 {
		createCmd = append(createCmd, append([]string{"--exclude-nodes"}, *exclude...)...)
	}
	if *noController {
		createCmd = append(createCmd, "--no-controller")
	}

	out, err := exec.CommandContext(ctx, "kubectl", createCmd...).Output()
	if err != nil {
		log.Fatalf("failed to create sos-report: %s", err)
	}

	type LinstorMsg struct {
		ObjRefs struct {
			Path string `json:"path"`
		} `json:"obj_refs"`
	}
	var msgs []LinstorMsg

	err = json.Unmarshal(out, &msgs)
	if err != nil {
		log.Fatalf("failed to parse LINSTOR message: %s", err)
	}

	if len(msgs) != 1 {
		log.Fatalf("expected exactly one LINSTOR message, got %d", len(msgs))
	}

	if msgs[0].ObjRefs.Path == "" {
		log.Fatalf("LINSTOR message does not have sos-report path")
	}

	var dest string
	if len(flags.Args()) == 0 {
		dest = filepath.Base(msgs[0].ObjRefs.Path)
	} else {
		s, err := os.Stat(flags.Arg(0))
		if os.IsNotExist(err) {
			dest = flags.Arg(0)
		} else if s.IsDir() {
			dest = path.Join(flags.Arg(0), filepath.Base(msgs[0].ObjRefs.Path))
		} else {
			dest = filepath.Base(msgs[0].ObjRefs.Path)
		}
	}

	// --retries=-1 is needed as SOS reports can be quite large: https://github.com/kubernetes/kubernetes/issues/60140
	err = exec.CommandContext(ctx, "kubectl", "cp", "--retries=-1", fmt.Sprintf("%s/%s:%s", namespace, pod, msgs[0].ObjRefs.Path), dest).Run()
	if err != nil {
		log.Fatalf("failed to copy SOS report to host: %s", err)
	}

	err = exec.CommandContext(ctx, "kubectl", append(kubectlArgs, "rm", msgs[0].ObjRefs.Path)...).Run()
	if err != nil {
		log.Fatalf("failed to remove sos-report from container: %s", err)
	}

	fileInfo, _ := os.Stdout.Stat()
	color := ""
	resetColor := ""
	if (fileInfo.Mode() & os.ModeCharDevice) != 0 {
		color = "\x1b[1;32m"
		resetColor = "\x1b[0m"
	}

	fmt.Printf("%sSUCCESS:%s\n    File saved to: %s\n", color, resetColor, dest)
}

func rawExec(ctx context.Context, kubectlArgs []string, args ...string) {
	kubectlArgs = append(kubectlArgs, "linstor")
	for _, arg := range args {
		kubectlArgs = append(kubectlArgs, expandSpecialArgToLinstorResourceNames(ctx, arg)...)
	}

	cmd := exec.CommandContext(ctx, "kubectl", kubectlArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
	os.Exit(cmd.ProcessState.ExitCode())
}

var version = "0.0.0"

func main() {
	cmdArgs := os.Args[1:]

	if isVersion(cmdArgs...) {
		fmt.Println(version)
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	namespace, podName, err := getControllerPodNamespacedName(ctx)
	if err != nil {
		log.Fatalf("%s", err)
	}

	toExecArgs := []string{"exec", "--namespace", namespace, "--stdin"}
	if fileInfo, _ := os.Stdout.Stat(); (fileInfo.Mode() & os.ModeCharDevice) != 0 {
		// Enable interactive mode in case the plugin is running in a tty.
		toExecArgs = append(toExecArgs, "--tty")
	}

	toExecArgs = append(toExecArgs, fmt.Sprintf("pod/%s", podName), "--")
	switch {
	case isSosReportDownload(cmdArgs...):
		doSosReportDownload(ctx, namespace, podName, toExecArgs, cmdArgs...)
	default:
		rawExec(ctx, toExecArgs, cmdArgs...)
	}
}
