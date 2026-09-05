---
title: "Godot 4 + C# Pitfall Notes: Five Deep Traps, with a Cheat Sheet and Minimal Reproductions"
description: Five of the most subtle Godot 4 + C# traps (script loading, config overwrites, build pollution, test runs, naming conflicts), with source-level root-cause analysis, a cheat sheet, and engineering scaffolding — for developers who can already compile and run.
date: 2026-08-23
level: P5
series: ""
tags:
  - Godot
  - C#
  - .NET
  - Compiler
  - Engineering
---

> Environment: Godot 4.7.2 (mono) / .NET SDK 9 / C# (net8.0) / Linux
>
> 🎯 Audience: **developers who can already compile and run a Godot C# project and are currently debugging script loading or build issues**. Beginners: follow the official "Hello World" tutorial first, then come back. This article assumes you know your way around csproj files and a terminal; sections with unfamiliar terms can be skipped without losing the main thread.
>
> 🔧 The collapsible blocks below are **source-level deep dives** — feel free to expand them — they're the real meat of this article. The main thread only needs "symptom → fix".
>
> 📌 Not covered here (our own workflow never hit these, and **we don't write about traps we haven't stepped in**; contributions welcome):
>
> - Hot Reload / runtime debugging
> - Community-known traps like `Godot.Collections.Array<T>` generic constraints or `StringName` null behavior
> - C# traps on the mobile/web export pipeline

## 🚨 Cheat Sheet (read this first)

| #   | What you did                               | Symptom keywords                                 | One-line fix                                                                         |
| :-- | :----------------------------------------- | :----------------------------------------------- | :----------------------------------------------------------------------------------- |
| 1   | Created/renamed a C# script                | Script "class not found / does not inherit Node" | **Filename (case-sensitive) must exactly match the class name (PascalCase-aligned)** |
| 2   | Edited project.godot externally            | Config silently overwritten, clicks dead         | **Close the editor before editing project.godot**                                    |
| 3   | Built repeatedly / built while editor open | "Duplicate attribute CS0579"                     | **Close editor + exclude sibling projects' bin/obj in csproj**                       |
| 4   | Ran unit tests (xUnit etc.)                | "You must install or update .NET"                | **Add `<RollForward>LatestMajor</RollForward>` to test projects**                    |
| 5   | Wrote a `Timer` variable                   | CS0104 ambiguous reference                       | **Fully qualify `Godot.Timer`**, or `<ImplicitUsings>disable</ImplicitUsings>`       |

## 🛡️ Survival Rules (6)

1. C# filename = class name (PascalCase, case-sensitive)
2. Close the editor before editing `project.godot`
3. Avoid CLI builds while the editor is open (**avoiding concurrent writes is the key** — locally, closing the editor before building is the least hassle; in CI there's no editor, so builds are inherently safe; see trap 3 for the suspend trick)
4. Add `<RollForward>LatestMajor</RollForward>` to test/console projects
5. Fully qualify Godot types (`Godot.Timer`), or disable ImplicitUsings
6. In multi-project roots, exclude other projects' bin/obj and `.godot/**`

## 🛡️ Preventive Coding Standards (reactive debugging → proactive avoidance)

- **Naming**: C# scripts are always PascalCase and match the class name; create new scripts via the editor's "New Script" template, never hand-write filenames
- **Config**: edit `project.godot` only through the editor's Project Settings panel; if you must text-edit it, close the editor first
- **Builds**: avoid CLI builds while the editor is open; build scripts (CI) should handle the editor state (close or suspend) up front
- **Multi-project**: every root-level csproj must exclude other projects' bin/obj and `.godot/**`
- **Namespaces**: when a Godot type collides with a BCL type, always fully qualify — or adopt a single `ImplicitUsings` policy

---

## 🚀 One-Click Scaffolding: Three Files (don't want to read the trade-offs? Just copy these)

Want the easy path: drop these three files into your project root and you're done. Want to understand why: see traps 3 and 4.

### ① `Directory.Build.props`

```xml
<!-- Place in the project root; MSBuild auto-imports it into every project below -->
<Project>
  <PropertyGroup>
    <DefaultItemExcludes>$(DefaultItemExcludes);.godot/**;engine/bin/**;engine/obj/**;tests/**;tools/**</DefaultItemExcludes>
  </PropertyGroup>
  <ItemGroup>
    <Compile Remove=".godot/**" />
    <Compile Remove="engine/bin/**" />
    <Compile Remove="engine/obj/**" />
  </ItemGroup>
</Project>
```

### ② `build.sh`

```bash
#!/usr/bin/env bash
set -e
# 1. Detect whether the Godot editor is running (it auto-builds and races the CLI on .godot/mono)
if pgrep -f "Godot.*--editor" > /dev/null; then
  echo "⚠️ Godot editor detected. Close it before building (avoid concurrent writes)"
  exit 1
fi
# 2. Build + test
dotnet build tianxing.sln
dotnet test tianxing.sln
```

> 🔎 **A more general detection approach**: process-name patterns vary by platform/distribution (the Steam build of Godot may use a different process name). Replace `"Godot.*--editor"` in `build.sh` with your own pattern (e.g. a `GODOT_PROC_PATTERN` env var); when unsure, self-check first: `ps aux | grep -i godot` (Linux/macOS) or `Get-Process | Where-Object {$_.ProcessName -like "*godot*"}` (Windows). The core principle is simply: **no other process should be writing to `.godot/mono` while you build**.
>
> 🪟 **Windows (PowerShell)**: use `build.ps1` — `build.sh` only applies to Linux/macOS:

```powershell
# build.ps1
# 1. Detect the Godot editor process (auto-build races the CLI on .godot/mono)
if (Get-Process -Name "Godot*" -ErrorAction SilentlyContinue) {
    Write-Host "⚠️ Godot editor detected. Close it before building (avoid concurrent writes)"
    exit 1
}
# 2. Build + test
dotnet build tianxing.sln
dotnet test tianxing.sln
```

### ③ `.gitignore` additions

```gitignore
bin/
obj/
.godot/mono/temp/
*.user
```

---

## Trap 1: Filename == Class Name (PascalCase) — Otherwise the Script Is Silently Ignored

**Symptom**: the autoload script won't start; headless runs fail the same way:

```text
ERROR: Failed to instantiate an autoload, script 'res://autoload/game_manager.cs' does not inherit from 'Node'.
```

```text
ERROR: Cannot instantiate C# script because the associated class could not be found.
Make sure the script exists and contains a class definition with a name that matches
the filename of the script exactly (it's case-sensitive).
```

**Minimal reproduction**: filename `game_manager.cs`, class name `GameManager` (looks perfectly normal, but always fails):

```csharp
// File: res://autoload/game_manager.cs
using Godot;

public partial class GameManager : Node { }
```

❌ Dead ends we tried: three namespace variations (`Tianxing.GameManager` → `Autoload.GameManager` → no namespace), all failed; `--verbose` confirmed the assembly loads fine, yet the class can't be found.

<details>
<summary>🔧 Deep dive: root cause (source-level) — skippable for beginners; platforms without folding support will just show it inline</summary>

Godot's source generator `ScriptPathAttributeGenerator.cs` only emits a `[ScriptPath]` registration for classes whose "filename (without extension) == class name":

> In plain words: classes whose filename doesn't match the class name are filtered out right here and never get any registration code generated.

```csharp
.Where(x =>
    // Ignore classes whose name is not the same as the file name
    Path.GetFileNameWithoutExtension(x.cds.SyntaxTree.FilePath) == x.symbol.Name)
```

`game_manager.cs` with a `GameManager` class → filename ≠ class name → the class is **silently ignored**, no registration is generated, and at runtime you naturally get "class not found" — with a highly misleading error message.

Note: if a class is split across multiple `partial` files, the filter runs per syntax tree — **as long as at least one file has a filename that matches** the class name, the class gets registered; the other partial files simply don't participate in registration. **Recommendation: keep the main class file aligned with the filename, and don't declare classes in other partial files whose names don't match their filenames** (it makes debugging needlessly confusing).

</details>

**Fix**:

```bash
git mv autoload/game_manager.cs autoload/GameManager.cs   # just align the filename with the class name
```

✅ **TL;DR**: the filename (case-sensitive) of a Godot C# script must match the class name exactly, or the script is silently discarded.

---

## Trap 2: Close the Editor Before Editing project.godot — Otherwise Your Config Gets Overwritten by the Stale In-Memory Copy

**Symptom**: you press F5 in the editor and nothing is clickable; the log has one line:

```text
ERROR: System.NullReferenceException ... at GameManager.Instance...
```

`git diff project.godot` shows the file was rewritten wholesale: the `[autoload]` section deleted (singleton gone, clicks naturally dead), the renderer reverted, window size lost.

**Minimal reproduction**:

1. Open the Godot editor and load the project
2. Modify `project.godot` externally (e.g. add an `[autoload]` section)
3. The editor saves the project from the editor side (e.g. applying a change in the Project Settings window, or setting a main scene) → the on-disk file is overwritten by the stale in-memory copy, your changes vanish

**Root cause**: the editor holds an in-memory copy of `project.godot` while a project is open; whenever the project settings are saved from the editor, it writes back from its own stale copy — **external changes are overwritten by the stale in-memory copy**. Since Godot 4.x (verified on 4.7.2), the editor usually warns when it detects the file changed on disk: when the editor window regains focus it pops up a "Files have been modified outside Godot" dialog offering "Reload from disk" or "Ignore external changes". **But the warning does not block later saves** — if you ignore the dialog and then save any project setting from the editor (including clicking "Ignore external changes" in that dialog), the external changes are still silently discarded. Quitting the editor does not write `project.godot` back by itself (nothing to do with version control; pure editor behavior).

### Fix (safe modification workflow)

- ✅ Recommended: edit via the editor's **Project Settings** panel
- ✅ Or: close the editor → edit with a text editor → reopen the editor (only now will it read the new config)

```bash
# Close editor → edit project.godot → reopen editor
```

✅ **TL;DR**: `project.godot` should only ever be changed by the editor itself; for external edits, close the editor first (otherwise your changes get clobbered by the editor's stale in-memory copy).

---

## Trap 3: CS0579 Duplicate Attributes — Build Pollution Needs Excludes + Isolated Builds

**Symptom**:

`dotnet build` fails reliably, pointing at generated files under `.godot/mono/temp/obj/`:

```text
error CS0579: Duplicate 'System.Reflection.AssemblyCompanyAttribute' attribute
error CS0579: Duplicate 'global::System.Runtime.Versioning.TargetFrameworkAttribute' attribute
```

**Minimal reproduction**:

1. A solution root contains a Godot project (Godot.NET.Sdk, intermediates in `.godot/mono/temp/obj`) and a sibling project, e.g. `tests/` with plain Microsoft.NET.Sdk
2. Build the solution → the sibling's generated files land in `tests/obj/**`
3. Build the solution again → the Godot project's default `**/*.cs` glob sweeps in the sibling's leftover `tests/obj/**/*.cs` (generated `AssemblyInfo.cs`, global usings, etc.), duplicating what the SDK generates this time → CS0579, with errors pointing at `.godot/mono/temp/obj/`
4. If the Godot editor is open (it auto-builds), it races the CLI on the same directory, worsening the situation

<details>
<summary>🔧 Deep dive: root cause (source-level) — skippable for beginners; platforms without folding support will just show it inline</summary>

A project's default `**/*.cs` compile glob includes every `.cs` file under the project folder, minus `$(DefaultItemExcludes)` and hidden directories. The Godot project's own intermediates are safe: `DefaultItemExcludes` automatically includes `$(BaseIntermediateOutputPath)/**` — which Godot.NET.Sdk redirects to `.godot/mono/temp/obj/` — so a single Godot project never re-collects its own generated files (verified: the exclude is present in .NET SDK 5.0 through 10.0).

The real trap is sibling projects: their leftover `bin/`/`obj/` generated files (`tests/obj/**` etc.) are not covered by the Godot project's excludes, so they get swept into its compilation and duplicate the assembly attributes the SDK generates this round. The errors point at `.godot/mono/temp/obj/`, which is misleading. Clearing caches only treats the symptom.

</details>

### Fixes (ordered by safety)

① **Most reliable: close the editor and build**. No concurrent writes, no pollution source.

② **Suspend the editor before CLI builds** (🛠️ advanced trick, not required; Linux/macOS only):

```bash
pgrep -af Godot          # find the editor PID
kill -STOP <PID>         # suspend (freeze)
dotnet build tianxing.sln
kill -CONT <PID>         # resume
```

> ⚠️ **Warning**: `kill -STOP/CONT` is Linux/macOS only — **not available on Windows**; if the editor UI misbehaves after resuming (rendering/input stuck), just restart the editor (project state is not lost). Also note: **suspending for too long** may cause the GPU context (Vulkan/OpenGL) to reset on resume and crash the editor (possible loss of editor state, not data). **Core principle: avoid concurrent writes** — locally, closing the editor before building is the least hassle (in CI there's no editor, so builds are inherently safe). If you don't need to preserve editor state (scene layout, dock panels, etc.), just close it; this trick is only for when you really don't want to reopen the editor.
>
> 🪟 **Windows users**: just close the editor and build (most reliable), or use the in-editor build (F5 auto-builds); CLI builds are not the normal path on Windows.

③ **Global excludes: Directory.Build.props** (recommended for multi-project, best practice):

```xml
<!-- Root-level Directory.Build.props: MSBuild auto-imports into every project below, one-shot global excludes -->
<Project>
  <PropertyGroup>
    <DefaultItemExcludes>$(DefaultItemExcludes);.godot/**;engine/bin/**;engine/obj/**;tests/**;tools/**</DefaultItemExcludes>
  </PropertyGroup>
  <ItemGroup>
    <Compile Remove=".godot/**" />
    <Compile Remove="engine/bin/**" />
    <Compile Remove="engine/obj/**" />
  </ItemGroup>
</Project>
```

> 📦 Place it in the **project root**; every subproject (including engine/, tests/) inherits it automatically; exclude paths resolve **relative to each project's own root**, so non-existent directories are harmless. Multi-project maintenance cost drops sharply — zero config for new projects. If you want it to affect only a single project, use option ④'s per-csproj excludes (more explicit).

④ **Local excludes: explicit per-csproj config** (more explicit; enough for single-project setups):

```xml
<!-- Exclude paths are relative to the project root -->
<PropertyGroup>
  <DefaultItemExcludes>$(DefaultItemExcludes);.godot/**;engine/bin/**;engine/obj/**;tests/**;tools/**</DefaultItemExcludes>
</PropertyGroup>
<ItemGroup>
  <Compile Remove=".godot/**" />
  <Compile Remove="engine/bin/**" />
  <Compile Remove="engine/obj/**" />
</ItemGroup>
```

> 💡 We verified `DefaultItemExcludes` works on our setup; still, using it together with `Compile Remove` as **belt and suspenders** is recommended (if an older Godot.NET.Sdk doesn't honor the property, `Compile Remove` has your back). If the excludes still don't take effect: with the SDK shorthand (`<Project Sdk="...">`) the project body is already positioned before default-item evaluation; with explicit `<Import>` style projects, place the PropertyGroup after the Sdk.props import. **Exclusion mechanics may differ across Godot.NET.Sdk versions — defer to the docs for your version** (we verified traditional excludes on 4.7.2).
>
> 📦 **Multi-csproj projects**: if you don't use Directory.Build.props (option ③), every root-level csproj must add the excludes **individually** — a sibling's generated outputs (`tests/obj/**`, `engine/obj/**`, etc.) can be swept in by the Godot project's default glob (`.godot/**` itself is already excluded as a hidden directory on current .NET SDKs).

> 📤 **Export scenario**: to package, use `godot --headless --export-release <preset>` (you must configure an export preset in the editor first). Note: a headless export **also triggers a C# build internally** (via the editor build callback), so the concurrent-write risk with a GUI editor still exists — **close the GUI editor before exporting** too.

(There's also a community trick of symlinking `.godot/mono/temp` to `/tmp` to isolate it; we haven't verified it, so we won't recommend it.)

✅ **TL;DR**: in multi-project solutions, a sibling project's leftover `bin/`/`obj/` files get swept into the Godot project's compile glob and duplicate its assembly attributes (the errors misleadingly point at `.godot/mono/temp/obj/`); the most reliable fix is "close the editor before building + exclude sibling projects' folders in csproj".

---

## Trap 4: Test Host Missing the .NET 8 Runtime — RollForward Declaration

**Symptom**: the test project compiles but crashes the moment it runs:

```
Testhost process exited with error: You must install or update .NET to run this application.
Framework: 'Microsoft.NETCore.App', version '8.0.0' (x64)
The following frameworks were found: 9.0.19 at [...]
```

**Minimal reproduction**: only the .NET 9 runtime is installed, and you run `dotnet test` on a net8.0-targeted xUnit project.

**Root cause**: the .NET SDK 9 **can compile** net8.0 targets (the compiler is forward-compatible), but **running testhost requires the net8.0 runtime**; the SDK won't roll forward to a major version on its own.

**Fix**: declare roll-forward in the test csproj:

```xml
<PropertyGroup>
  <RollForward>LatestMajor</RollForward>
</PropertyGroup>
```

> Note: `RollForward` only affects **runtime** version selection; it does not change the **compile-time** target framework (`<TargetFramework>net8.0</TargetFramework>` stays as-is). Rolling forward to .NET 9 carries a **very small risk of API behavior differences**; fine for development, but **deploy the correct runtime in production**.
>
> 💡 **CI advice**: in critical environments like CI/CD, the **best practice is to pin the SDK via `global.json` and install the target runtime, rather than relying on `RollForward`** — avoid implicit roll-forward, prevent "works locally, fails in CI", and keep behavior fully reproducible.

✅ **TL;DR**: when the target framework doesn't match the installed runtime, add `<RollForward>LatestMajor</RollForward>` to the projects that need to run.

---

## Trap 5: Timer Name Collision — Fully Qualify Godot Types

**Symptom**:

```
error CS0104: 'Timer' is an ambiguous reference between 'Godot.Timer' and 'System.Threading.Timer'
```

**Minimal reproduction**: a C# project with `ImplicitUsings` enabled (which implicitly brings in `System.Threading`), writing `new Timer { ... }` alongside `using Godot;`.

**Root cause**: both namespaces define a `Timer`; the compiler can't decide for you.

### Fix (pick one)

① Fully qualify both the field and the constructor:

```csharp
private Godot.Timer _timer = null!;
_timer = new Godot.Timer { OneShot = true, WaitTime = 2.0f };
```

② If the project uses `System` types heavily and qualifying everything is tedious, disable implicit usings and add the ones you need manually:

```xml
<PropertyGroup>
  <ImplicitUsings>disable</ImplicitUsings>
</PropertyGroup>
```

> Note: Godot 4's `Timer.WaitTime` is a `double` (seconds); if you actually meant `System.Timers.Timer` or `System.Threading.Timer`, those are a completely different API (callback model, threading semantics) — don't mix them up.

✅ **TL;DR**: when a Godot type collides with a BCL type, fully qualify it — or disable ImplicitUsings project-wide.

---

## Appendix 1: Debugging Tools (how we actually located these)

1. **`godot --headless --verbose`**: the startup log prints .NET module init, API hashes, and assembly paths — the fastest way to confirm whether "the assembly even loaded"
2. **Read the Godot source directly** (when the official docs fail you):
   - `modules/mono/editor/Godot.NET.Sdk/Godot.SourceGenerators/ScriptPathAttributeGenerator.cs` — trap 1's root cause (key filter at `#L54-L57`: [blob link](https://github.com/godotengine/godot/blob/4.7.2-stable/modules/mono/editor/Godot.NET.Sdk/Godot.SourceGenerators/ScriptPathAttributeGenerator.cs#L54-L57))
   - `modules/mono/glue/GodotSharp/GodotSharp/Core/Bridge/ScriptManagerBridge.cs` — the path→type registration mechanism
   - `modules/mono/godotsharp_dirs.cpp` / `modules/mono/mono_gd/gd_mono.cpp` — assembly directory & loading logic (corroborates trap 3)
   - Fetching: `https://raw.githubusercontent.com/godotengine/godot/<version-tag>/modules/mono/...` (a release tag such as `4.7.2-stable` is pinned to the commit it was released from, safe to reference; for absolute stability, swap in a commit hash yourself)
3. **`pgrep -af Godot` + `kill -STOP/CONT`**: handle the editor's concurrent builds (suspend rather than kill)
4. **Divide and conquer**: shrink the problem to the smallest scenario (one script, one scene, one build) before locating it

## Appendix 2: Order of Checks for Weird C# Errors

Check in this order and you'll locate the vast majority of issues within five minutes:

1. **Filename == class name?** (case-sensitive) — the most common reason a script is silently ignored
2. **Was `project.godot` changed externally?** — `git diff project.godot` for unexpected regressions (autoload/rendering/window settings)
3. **Is a build directory being swept in by a glob?** — confirm the csproj excludes `.godot/**` and other projects' bin/obj
4. **Does the runtime version match?** — add `RollForward` to test/console projects, or install the matching runtime
5. **Namespace collision?** — fully qualify Godot types that collide with BCL types, or adopt a single ImplicitUsings policy
