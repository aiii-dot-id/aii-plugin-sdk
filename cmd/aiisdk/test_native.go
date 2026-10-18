package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
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
		w, err := workerdrive.StartNative(carrier, env, timeout, h.answer)
		if err != nil {
			rep.fail("readiness", err.Error())
			return
		}
		banner, closeIt = w.Banner(), w.Close
		invoke = func(n int, operation string, args json.RawMessage) (*workerdrive.Reply, error) {
			return w.Invoke(fmt.Sprintf(`"check-%d"`, n), operation, args, timeout)
		}
	}
	if !strings.Contains(banner, " sdk=") {
		rep.fail("readiness", "the ready line names no kit (sdk=): "+banner)
	} else {
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
