package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aiii-dot-id/aii-plugin-sdk/internal/workerdrive"
	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

func testNative(dir string, cfg *aiiospkg.AuthorConfig, rep *report, h *harness, timeout time.Duration, modelsDir, runtimeRoot string) int {
	variant := nativeVariant(cfg)
	if variant == nil {
		platform, arch := hostPlatformArch()
		rep.incompleteLocal("native", fmt.Sprintf("the plugin declares no native variant for %s/%s", platform, arch), "declare one for this machine in plugin.json, or run the wasm flow")
		rep.incompletePrereq = true
		return finish(rep, h)
	}
	if variant.Artifact == "" {
		rep.fail("native", "variant "+variant.VariantID+" names no artifact: a native variant's carrier is yours to build and name")
		return finish(rep, h)
	}
	if code := cmdPackage(nil); code != 0 {
		rep.fail("package", "aiisdk package failed (above)")
		return finish(rep, h)
	}
	rep.pass("package", bundlePath(dir, cfg))
	rep.packageHash, rep.manifestHash = stagedSigningInputs(dir, cfg)
	runNative(dir, cfg, rep, h, variant, timeout, modelsDir, runtimeRoot)
	return finish(rep, h)
}

func runNative(dir string, cfg *aiiospkg.AuthorConfig, rep *report, h *harness, variant *aiiospkg.AuthorVariant, timeout time.Duration, modelsDir, runtimeRoot string) {
	env := []string{"SEV_PLUGIN_SOCKET=stdio:", "SEV_PLUGIN_ID=" + cfg.ID}
	if modelsDir != "" {
		env = append(env, "AII_MODELS_DIR="+modelsDir)
	}
	if runtimeRoot != "" {
		env = append(env, "AII_RUNTIME_ROOT="+runtimeRoot)
	}
	carrier := filepath.Join(dir, filepath.FromSlash(variant.Artifact))
	awaited := cfg.PluginFamily == "voice_interface" || variant.Accelerator != nil

	var banner string
	var invoke invoker
	var session sessionRun
	var closeIt func(time.Duration) error
	if cfg.PluginFamily == "voice_interface" {
		sw, err := workerdrive.StartNativeSession(carrier, env, timeout, h.answer)
		if err != nil {
			rep.fail("readiness", err.Error())
			return
		}
		banner, closeIt = sw.Banner(), sw.Close
		invoke = func(_ int, operation string, args json.RawMessage) (*workerdrive.Reply, error) {
			return sw.Invoke(operation, args, timeout)
		}
		session = newSessionDriver(sw).run
	} else {
		var w *workerdrive.Worker
		var err error
		if awaited {
			w, err = workerdrive.StartNative(carrier, env, timeout, h.answer)
		} else {
			w, err = workerdrive.StartNativeUnprofiled(carrier, env, h.answer)
		}
		if err != nil {
			rep.fail("readiness", err.Error())
			return
		}
		banner, closeIt = w.Banner(), w.Close
		invoke = func(n int, operation string, args json.RawMessage) (*workerdrive.Reply, error) {
			return w.Invoke(fmt.Sprintf(`"check-%d"`, n), operation, args, timeout)
		}
	}
	switch {
	case !awaited:
		rep.pass("readiness", "started; variant "+variant.VariantID+" declares no accelerator profile, so a host waits for no ready line and none was awaited")
	case readyRefusal(banner, variant.Accelerator != nil) != "":

		rep.fail("readiness", readyRefusal(banner, variant.Accelerator != nil))
		_ = closeIt(timeout)
		return
	default:
		rep.pass("readiness", strings.TrimSpace(banner))
	}
	if cfg.ValidationFile == "" {
		rep.note("checks", "the plugin declares no checks; a host uses it on its readiness alone")
	} else {
		runPackageChecks(rep, h, dir, cfg, variant.VariantID, invoke, session)
	}
	if cerr := closeIt(timeout); cerr != nil {
		rep.fail("exit", cerr.Error())
	} else {
		rep.pass("exit", "clean stdin-EOF exit 0")
	}
}

var profileReadyFields = []string{"models_loaded", "accelerator", "probe_ms"}

func readyRefusal(banner string, profiled bool) string {
	if !profiled {
		return ""
	}
	models, probe, accelerator := 0, -1, ""
	for _, f := range strings.Fields(banner) {
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			continue
		}
		switch k {
		case "models_loaded":
			models, _ = strconv.Atoi(v)
		case "accelerator":
			accelerator = v
		case "probe_ms":
			if n, err := strconv.Atoi(v); err == nil {
				probe = n
			}
		}
	}
	have := map[string]bool{"models_loaded": models > 0, "accelerator": accelerator != "", "probe_ms": probe >= 0}
	var missing []string
	for _, k := range profileReadyFields {
		if !have[k] {
			missing = append(missing, k+"=")
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return "the ready line of a variant that declares an accelerator profile lacks valid " + strings.Join(missing, ", ") + " (positive models_loaded, nonempty accelerator, nonnegative probe_ms), which a host requires of it: " + strings.TrimSpace(banner)
}
