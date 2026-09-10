---
title: "Environment & Operations: Installing Ollama on an Intel Arc A770 and Keeping It Stable Long-Term (Part 1)"
description: "Getting Ollama installed on an Intel Arc A770 and keeping it stable over the long run: mirror install, lighting up the GPU through Vulkan, model selection, measured performance, and a long-term operations handbook (monitoring, environment variables, troubleshooting order) that is required reading before going live."
date: 2026-09-08
series: go-agent
order: 1
tags:
  - Agent
  - LLM
---

This series builds an agent locally in Go, without touching Python/Node at any point (readers working in Python or another language can still read the mechanism and pitfall sections of every part — tool-calling mechanics, chat templates, API compatibility and operations concepts are language-independent). Part 1 covers environment and operations only, with no code: install Ollama, light up Vulkan, pull the models, and explain clearly which pitfalls long-running deployments hit and how to monitor them; the code starts in Part 2.

- Prerequisites: a basic Linux command line, and enough familiarity to follow systemd service concepts (`systemctl`/`journalctl`)
- Heads-up: Section 5, "the long-term operations handbook", is **ops-facing content (an SRE/DevOps view)** — pure Go developers can skip it for now (it does not affect Parts 2–5), but it is the most distinctive stability knowledge in this series; before running long-term in production, be sure to come back and read it closely

> Environment note: this article is based on measurements of **Ollama 0.33.3 + llama3.1:8b (A770/Vulkan, 2026-09)**; Ollama iterates fast, so treat `ollama serve --help` as the source of truth for environment variables, and the [official OpenAI compatibility docs](https://docs.ollama.com/api/openai-compatibility) as the source of truth for the `/v1` compatibility fields.

## Background

I'm a Nuxt + Go full-stack developer who didn't want to learn another language just to study agents, so I went straight at it with the Go I already know.

Hardware: Intel Arc A770 16GB + 16GB of RAM (not a mainstream AI configuration, and the corresponding pitfalls are covered below).

## 1. Installing Ollama

The commands and auto-start examples in this article assume Linux (systemd). **Windows/macOS users**: the official Windows build of Ollama configures environment variables and start-at-boot in system settings and has no `systemctl`/`journalctl`; models and the later code sections are unaffected by platform. macOS has no A770 equivalent, but the code parts still run as written (the Metal backend takes over automatically); see the [official installation docs](https://docs.ollama.com) for the details.

> This tutorial requires **Ollama >= 0.3.0**, which guarantees native support for the OpenAI-compatible `/v1/chat/completions` endpoint (older versions leave it disabled by default, and the code will return 404/400).

> ⚠️ Note: that `/v1` endpoint is OpenAI's **"approximately compatible" implementation, not a field-by-field equivalent** — the official compatibility docs do not list `tool_choice`, `n`, `logit_bias` and similar fields as supported (`tool_choice` behaves partly by version; see Part 2, Section 1). For the specific differences and the pre-migration self-test checklist, see "Interface baseline" at the start of Part 2.

Failed installs are almost always network problems (interrupted downloads, incomplete files), and there are only two fixes: **turn on a VPN** or **switch mirrors**.

Installing through a mirror:

```bash
export OLLAMA_MIRROR="https://ghproxy.cn/https://github.com/ollama/ollama/releases/latest/download"
curl -fsSL https://ollama.com/install.sh | sed "s|https://ollama.com/download|$OLLAMA_MIRROR|g" | sh
```

If you get `llama-server binary not found` (an incomplete install), delete it and reinstall:

```bash
sudo rm -rf /usr/local/lib/ollama
curl -fsSL https://ollama.com/install.sh | sh
```

Once the install finishes, verify the service:

```bash
curl http://localhost:11434
```

The install script will report `No NVIDIA/AMD GPU detected` — Intel GPUs are not detected by default, and the next step fixes that.

---

## 2. Enabling Intel Arc A770 GPU acceleration

Ollama natively prefers NVIDIA (CUDA) and AMD (ROCm). Intel Arc has to go through the **Vulkan backend**.

### Configuration

```bash
sudo systemctl edit ollama
```

Fill in:

```ini
[Service]
Environment="OLLAMA_VULKAN=true"
```

Save and restart:

```bash
sudo systemctl daemon-reload
sudo systemctl enable ollama   # start at boot (most install scripts already enable it; this command is idempotent, run it freely)
sudo systemctl restart ollama
```

> That single variable is all it takes. The `OLLAMA_INTEL_GPU`, `OLLAMA_NUM_GPU_LAYERS` and similar variables floating around online do not appear in Ollama's officially supported list (support can change between versions, so treat the environment-variable list printed by `ollama serve --help` as authoritative). Note: in some versions (including the 0.33.3 used here) `OLLAMA_VULKAN` does not show up in that list, yet the variable is recognized and does take effect — don't doubt your configuration just because it isn't listed. Once Vulkan is set, Ollama loads every layer of the model onto the GPU automatically.

### Verification

```bash
journalctl -u ollama --no-pager | grep "inference compute" | tail -3
```

Seeing `Vulkan0 ... Intel Arc A770 Graphics` means success:

```
inference compute id=0 library=Vulkan name=Vulkan0
  description="Intel(R) Arc(tm) A770 Graphics (DG2)" type=discrete total="15.9 GiB"
```

> On WSL2 **without systemd** (see Section 5.6 for how to enable it), or in pure-container and other non-systemd environments, just run `ollama serve` in the foreground and watch the terminal for the `inference compute` log line — the effect is the same.

> ⚠️ **The Vulkan backend carries known stability risks: getting it running is not the same as running it stably long-term.** Upstream (llama.cpp / Ollama) has recorded, for some Linux kernel + Mesa (Intel ANV) driver combinations, VRAM accounting drift, idle VRAM being swapped out, and occasional OOMs (e.g. [ollama #17802](https://github.com/ollama/ollama/issues/17802), [ollama #18272](https://github.com/ollama/ollama/issues/18272), [llama.cpp #18946](https://github.com/ggml-org/llama.cpp/issues/18946), [llama.cpp #25646](https://github.com/ggml-org/llama.cpp/issues/25646)), all of which keep getting fixed across Ollama/Mesa releases. For long-term deployments:
>
> - **Don't look only at VRAM usage at load time** (the table in Section 4 is a static snapshot); over a long run, watch whether the VRAM curve climbs monotonically or drops unexpectedly;
> - Monitoring: `journalctl -u ollama` for OOM/swap logs; on an Intel discrete GPU, `intel_gpu_top` or `xpu-smi` to watch VRAM;
> - Relevant environment variables (written in `sudo systemctl edit ollama`): `OLLAMA_LOAD_TIMEOUT` (how long a stalled model load waits before giving up, default 5m), `OLLAMA_KEEP_ALIVE` (how long an idle model stays loaded, default 5m), `OLLAMA_GPU_OVERHEAD` (VRAM reserved for the driver and other processes);
> - When something goes wrong, troubleshoot in order: upgrade Ollama (which also updates the llama.cpp backend) → update the Mesa driver → switch to the CUDA/ROCm or pure-CPU backend for comparison, to pin down whether it is a driver problem or a backend problem.

---

## 3. Choosing a model

This tutorial uses **`llama3.1:8b`** by default: it follows the tool-calling format more reliably, and an 8B quantized model still fits entirely in the A770's VRAM.

```bash
ollama pull llama3.1:8b
```

> Why not `qwen2.5-coder`? In our measurements that model wrote tool calls into plain-text `content` — that is a **model compliance** problem rather than a template problem, and it has nothing to do with "Qwen not supporting tool calling"; the full transcript, the conclusion and the template self-check are in **Appendix A** at the end. If you are on a newer Ollama, run `ollama show <model> --modelfile` first to self-check whether the template has been updated — template/model-layer problems can be fixed across releases.

> On memory: an 8B model (roughly 4.5GB at Q4 quantization) fits entirely in the A770's 16GB of VRAM (measured in Section 4 below). With the bulk of the model in VRAM, system memory usage is small (about 300MB in our measurements), so 16GB of RAM is plenty; you only need to worry about memory if you run several large models at once or raise the context size — when you see `killed`, close other large applications first, or switch to a smaller model (such as `qwen2.5:3b`).

---

## 4. Measured performance (Intel Arc A770 + llama3.1:8b)

> The data comes from measurements on this machine (Ollama 0.33.3), read from the model-loading and timing logs in `journalctl -u ollama`; environment snapshot (measured 2026-09): Ubuntu 24.04 LTS · kernel 7.0.0-31-generic · Mesa Vulkan driver 25.2.8 (intel-media-va-driver 24.1.0) · Arc A770 (DG2) — performance and stability vary with the kernel/Mesa combination, and the numbers in this article use that as their baseline.

> ⚠️ The table below is **static usage after loading**, which is not the same as long-term stability: on some kernel/Mesa driver combinations the Vulkan backend has known problems with VRAM accounting drift and idle VRAM being swapped out, so over a long run you should monitor the VRAM curve rather than this one moment — see Section 2 for how.

#### VRAM and memory usage

| Item              | Measured value                    | Notes                                                           |
| ----------------- | --------------------------------- | --------------------------------------------------------------- |
| GPU layer loading | **33/33 layers, fully offloaded** | The whole model sits on the A770, not a CPU hybrid              |
| Model VRAM        | ~4.4 GB                           | Ample headroom in the A770's 16GB of VRAM                       |
| KV Cache          | ~512 MB                           | The context cache lives in VRAM too                             |
| System memory     | ~300 MB                           | The bulk of the model is in VRAM; 16GB of RAM handles it easily |

#### Inference speed (measured range for generating 14–24 tokens)

| Metric                  | Measured value      |
| ----------------------- | ------------------- |
| Prompt processing       | ~470 ~ 700 tokens/s |
| Token generation (eval) | **~41 tokens/s**    |

> For comparison: running an 8B model on CPU alone usually yields single digits to low teens of tokens/s, so the A770's Vulkan acceleration clearly pays off. 41 tokens/s is enough for interactive conversation — usable, though hardly blazing fast.

> Aside: the speeds above come from the small requests described in Part 2 (a few dozen tokens), where wall-clock time is dominated by prompt processing rather than generation; the whole response completes in under a second.

> **Long context and the KV Cache**: the performance data above comes from short requests. In long-context scenarios the KV Cache grows linearly with the token count — at 32K tokens it can take several GB (16GB on the A770 is fine for everyday 8B interaction, but long-context scenarios need attention to VRAM headroom). You can cap the maximum context length with `OLLAMA_CONTEXT_LENGTH` (defaults adapt to available VRAM at 4k/32k/256k) to avoid running out of VRAM.

> 📌 **Only here for the code?** You've read enough — jump straight to Part 2. The operations material in Section 5 is for readers preparing to run long-term; there's no rush, come back to it later.

---

## 5. Long-term operations handbook (ops-facing · skippable for now, required reading before going live)

> This section is **ops-facing content** aimed at readers who need to deploy and maintain this machine over the long haul (an SRE/DevOps view): environment variables, monitoring and Mesa driver troubleshooting are operations knowledge. **Readers following the code for the first time can skip it** — it does not affect Parts 2–5 (Part 2 only needs "the model is pulled and the service is healthy"); but this section is the **most distinctive stability knowledge** in the series (the known Vulkan risks, the monitoring and the troubleshooting will never show up in the code on their own), so **before running long-term or going live, do come back and read it closely**.

> This section picks up the known risks from Section 2 and lays out a long-term operations process you can follow directly. The conclusion first: there is no shortcut to long-term stability — monitor the metrics, troubleshoot in order when something breaks, and upgrade to the official fixed version promptly.

### 5.1 Triage the symptom first (is it Vulkan's fault?)

| Symptom                                                         | More likely the fault of                            | Look at first                                                                                                        |
| --------------------------------------------------------------- | --------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| Generation slows down / stutters (tokens/s drops)               | VRAM swapped out / near capacity                    | Monitoring in Section 5.2; upstream record in [llama.cpp #25646](https://github.com/ggml-org/llama.cpp/issues/25646) |
| VRAM climbs monotonically, occasional OOM / `Not enough memory` | Vulkan/driver VRAM accounting problem               | Section 5.2 + upgrade Ollama/Mesa                                                                                    |
| The service crashes and exits outright                          | Usually a backend crash                             | The crash section in `journalctl -u ollama`                                                                          |
| Model load takes forever / times out                            | Stalled loading (slow driver, first shader compile) | Section 5.3, `OLLAMA_LOAD_TIMEOUT`                                                                                   |

### 5.2 The monitoring trio (commands and what to watch)

- **journalctl (primary)**: watch live with `journalctl -u ollama -f`; filter for anomalies with `journalctl -u ollama --no-pager | grep -iE "error|out of memory|failed"`, and pay attention to model load/unload lines and OOM sections;
- **intel_gpu_top (secondary)**: install `intel-gpu-tools` on Debian/Ubuntu and run it to watch the VRAM usage curve and render-engine occupancy. Run a long task for 30–60 minutes and confirm that VRAM usage falls back after requests instead of climbing monotonically;
- **Backup**: `xpu-smi` (if the Intel toolchain is installed) or simply the GPU statistics in Ollama's logs.

> Under Vulkan there is no standard VRAM tool like `nvidia-smi`; on this machine, journalctl as primary with intel_gpu_top as secondary is enough.

### 5.3 Environment variables quick reference (written into the systemd override)

```bash
sudo systemctl edit ollama
```

```ini
[Service]
Environment="OLLAMA_KEEP_ALIVE=10m"
Environment="OLLAMA_LOAD_TIMEOUT=10m"
Environment="OLLAMA_GPU_OVERHEAD=1073741824"
```

| Variable                   | Default   | Meaning / when to change it                                                                                      |
| -------------------------- | --------- | ---------------------------------------------------------------------------------------------------------------- |
| `OLLAMA_KEEP_ALIVE`        | 5m        | How long an idle model stays loaded. Raise it (e.g. 10m) under frequent calls to avoid repeated loading          |
| `OLLAMA_LOAD_TIMEOUT`      | 5m        | How long a stalled model load waits before giving up. Raise it with a slow driver or a long first shader compile |
| `OLLAMA_GPU_OVERHEAD`      | 0         | VRAM reserved for the driver/desktop processes (bytes). Reserving prevents OOM when VRAM is near full            |
| `OLLAMA_MAX_LOADED_MODELS` | 1 per GPU | Controls swapping models in and out when several share the same VRAM                                             |

> Treat the environment-variable list printed by `ollama serve --help` as the complete reference (Section 2 mentions this too; support can change between versions); whether `OLLAMA_VULKAN` appears in that list also depends on the version — see Section 2.

### 5.4 Troubleshooting order when something breaks

1. First decide whether it is a regression or an environment change: `journalctl -u ollama --since today` to find the most recent "healthy → unhealthy" inflection point;
2. **Upgrade Ollama** (which also updates the llama.cpp backend) → retest. Many Vulkan issues are annotated in release notes with the version that fixed them;
3. Still reproducing → update **Mesa / the kernel** (the Intel ANV driver lives in Mesa) → retest;
4. Still reproducing → **switch backends for comparison**: temporarily set `OLLAMA_VULKAN=false` and run once on pure CPU to tell a driver-path problem from a backend-logic problem;
5. After upgrading you still suspect an upstream bug → take a minimal reproduction (model, request, logs) and search for the same issue in [ollama/ollama](https://github.com/ollama/ollama/issues) or [llama.cpp](https://github.com/ggml-org/llama.cpp/issues).

How to check your own Mesa version: on Ubuntu/Debian use `dpkg -l | grep mesa-vulkan-drivers`; to see the runtime driver name, install `vulkan-tools` and run `vulkaninfo | grep -i driverName`. How to choose an upgrade target: upstream issues and release notes usually annotate "fixed in Mesa X.Y / Ollama vX", so that version is your target — no need to chase the newest one blindly. This article does not give a "universal minimum Mesa version": stability is tightly coupled to the kernel and to distribution packaging, so rely on measurements in your own environment and on upstream annotations.

### 5.5 Routine health check (run once before a release or a boot)

```bash
curl -s http://localhost:11434/api/version          # the service is alive
ollama list                                          # the models are all there
journalctl -u ollama --no-pager | grep "inference compute" | tail -1   # still Vulkan
ollama ps                                            # should be empty after 10 minutes idle (KEEP_ALIVE works)
```

### 5.6 Service management: auto-start, shutdown, and crash self-healing

Everything configured above assumes the service "is always running" — but **does it come back after a reboot?** Check three things first:

```bash
systemctl is-enabled ollama         # enabled = start-at-boot is on
systemctl status ollama             # active (running) + recent logs
sudo systemctl enable --now ollama  # if it isn't enabled, turn it on and start it now (idempotent; most install scripts already do this)
```

- When you don't need start-at-boot, turn it off with `sudo systemctl disable ollama`;
- The `ollama.service` installed by the official script ships with `Restart=always`: **systemd brings the process back automatically when it crashes**. So when you hit the "service crashed and exited" case from Section 5.1, run `journalctl -u ollama` to find the root cause first — don't skip it just because "it came back on its own";
- **On WSL2, prefer enabling systemd.** Newer WSL2 supports systemd: add `systemd=true` under `[boot]` in `/etc/wsl.conf`, then run `wsl --shutdown` and re-enter the distribution; from there everything matches this article (`systemctl enable --now ollama` and `journalctl -u ollama` work as written);
- **In environments where systemd is unavailable (WSL1, containers, …)**: keep a session alive in the background with `tmux new -d 'ollama serve'` (or `screen`), and `tmux attach` when you need the logs; `nohup ... &` is fine for a quick test but not recommended as a permanent setup.

---

## Appendix A: qwen2.5-coder tool calling, as it happened (why llama3.1 is the default)

> This appendix explains the "llama3.1 by default" decision in Section 3; a sample of the failing output is in the comparison subsection of "Run results" in Part 2.

What we saw: with the same code but `qwen2.5-coder:7b` (Ollama 0.33.3), the model wrote the tool call into `content` as JSON text instead of the standard `tool_calls` field. This is not Qwen failing to support Function Calling — whether Ollama parses `tools` into `tool_calls` correctly depends on the model's chat template working together with the backend. Our measurements point to two layers:

1. **The template layer (usually no longer a problem)**: the official `qwen2.5-coder:7b` TEMPLATE already carries the complete tool-calling format (the `<tool_call>` instruction block is visible in `ollama show qwen2.5-coder:7b --modelfile`);
2. **The model layer (the real trap)**: whether through `/v1`, the native `/api/chat`, or with `temperature=0`, this model reliably writes JSON into `content` — a **model compliance** problem (the community was still reporting the same in 2026), which changing the template cannot solve.

The conclusion depends on the model line: to keep using Qwen, first try **`qwen2.5` (the instruct version)** or retest after upgrading to a newer Ollama; to get something working fastest, use this tutorial's default `llama3.1:8b`.

> Scope of this conclusion: the transcript above is limited to **Ollama 0.33.3 + the official `qwen2.5-coder:7b`** (reproduced with `temperature=0` as well); other Ollama versions or sampling parameters (temperature/seed) may behave differently, so rely on measurements in your own environment.

### Self-check: does the model template carry the tool format?

- `ollama show <model> --modelfile | grep -n "Tools\|tool_call"`: seeing `.Tools` / `<tool_call>` means the template itself supports tool calling;
- Templates for official library models are updated as the library is maintained: `ollama pull` the latest first, then compare with `ollama show`;
- When you need a custom template or parameters, derive a new model with a Modelfile: `FROM <original model>` plus overriding `TEMPLATE` and `PARAMETER`, then `ollama create` and point the API at the new model name — note: the template only decides what the prompt looks like; whether the model complies is a separate matter.

---

## FAQ: quick answers (environment & operations)

| Problem                                                         | Cause                                                                                          | Fix                                                                                                                                  |
| --------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| The service fails to start with `llama-server binary not found` | Incomplete installation files                                                                  | `sudo rm -rf /usr/local/lib/ollama` and rerun the install script                                                                     |
| Port `11434` is already in use                                  | Another Ollama instance is running                                                             | Find and stop the old process with `ps aux \| grep ollama`                                                                           |
| `curl localhost:11434` cannot connect after a reboot            | The service is not set to start at boot                                                        | `sudo systemctl enable --now ollama` (for WSL2/no-systemd see Section 5.6)                                                           |
| The logs show all layers on CPU (0 layers offloaded)            | `OLLAMA_VULKAN` is not set, or the service was not restarted                                   | Configure it as in Section 2 and run `systemctl restart ollama`                                                                      |
| `killed` or out-of-memory at runtime                            | Several large models running at once, or a context size set too large                          | Close large applications, or switch to a smaller model                                                                               |
| VRAM climbs, OOM, or generation slows down over a long run      | The Vulkan backend has VRAM accounting/swap problems on some kernel + Mesa driver combinations | Upgrade Ollama and Mesa; monitor with `journalctl -u ollama` and `intel_gpu_top`; optionally set `OLLAMA_GPU_OVERHEAD` (Section 5.3) |
| The model seems stuck loading / no response for a long time     | Stalled loading caused by a driver or backend problem                                          | Set `OLLAMA_LOAD_TIMEOUT` and read the logs to locate it (Section 5.3)                                                               |

---

## Conclusion

1. **An A770 on Vulkan can fully offload an 8B model**: 33/33 layers, ~4.4GB of VRAM, ~41 tokens/s — the non-NVIDIA entry path holds up;
2. That completes the environment side: mirror install → Vulkan → model selection → measured performance → long-term operations handbook;
3. **This article is an introductory tutorial, not a production deployment template**: the Vulkan stability risks are real, and upgrading and monitoring are routine actions rather than optional ones (the risk list in Section 2 plus the handbook in Section 5, with Section 5 being required reading before deployment).

### Environment self-check for readers moving on to Part 2 (all green before you continue)

- [ ] `curl http://localhost:11434` returns normally
- [ ] `ollama list` includes `llama3.1:8b`
- [ ] `journalctl -u ollama | grep "inference compute"` shows Vulkan + Arc A770
- [ ] You know that `OLLAMA_LOAD_TIMEOUT`/`OLLAMA_KEEP_ALIVE`/`OLLAMA_GPU_OVERHEAD` exist and where to configure them

Next up: **"Minimal Code: The Complete Single-Tool, Single-Round Loop"** — writing your first Go agent.
