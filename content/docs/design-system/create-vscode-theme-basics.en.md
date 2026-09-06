---
title: 'VS Code Theme: From Handwritten JSON to Publishing'
description: Learn how to create, configure, and publish your own VS Code theme from scratch, without any scaffolding.
date: 2026-08-06 00:00:00
series: design-system
tags:
  - VSCode
  - Theme
  - Engineering
---

If you have some programming background (you are comfortable with JavaScript, JSON, and the command line), and you want to turn a color scheme into a VS Code theme without knowing where to start — this article is for you.

The Moongate theme started out as a color scheme for a personal blog. This article does not use any scaffolding. Instead, it **starts from a single, minimal JSON file** and gradually works through how a theme actually works. Once the mechanism is clear, you will naturally see which parts need engineering — and that is exactly what the rest of this series is about.

---

## 1. What a VS Code Theme Really Is

At the end of the day, a theme is just a single JSON file. It tells VS Code two things:

- **What the interface looks like**: the colors of the title bar, status bar, sidebar, editor background, and so on.
- **How code is colored**: which colors and styles are applied to different syntax elements (keywords, strings, comments, functions, and so on).

Understanding the file structure itself is not hard. What is genuinely hard is **knowing which key names to configure and what each one means**. So the core goal of this article is to walk you through creating a minimal working theme, and then show you how to use VS Code's built-in tools to find the correct key name for any color you want to tweak.

---

## 2. The Minimal Path to Your Own Theme

### 2.1 Create the project structure

Prepare an empty directory and create a `themes/` folder:

```bash
mkdir my-theme
cd my-theme
mkdir themes
```

Although a real extension project usually needs `package.json`, `README.md`, and other files, this article starts with the theme JSON itself so you can focus on the structure first.

### 2.2 Write a minimal theme JSON

Create a JSON file under `themes/`, for example `my-theme.json`:

```json
{
  "name": "My Theme",
  "type": "dark",
  "colors": {
    "editor.background": "#0f172a",
    "editor.foreground": "#e2e8f0"
  },
  "tokenColors": [
    {
      "name": "Comment",
      "scope": ["comment", "punctuation.definition.comment"],
      "settings": {
        "fontStyle": "italic",
        "foreground": "#a5b4cb"
      }
    },
    {
      "name": "Keyword",
      "scope": ["keyword", "storage.type", "storage.modifier"],
      "settings": {
        "foreground": "#3b82f6",
        "fontStyle": "bold"
      }
    },
    {
      "name": "String",
      "scope": ["string", "string.quoted.single", "string.quoted.double"],
      "settings": {
        "foreground": "#34d399"
      }
    }
  ]
}
```

This file is tiny, but it already contains every core component of a theme.

---

## 3. Core Concepts: `colors` and `tokenColors`

### `colors`: the editor interface

The `colors` object defines the **UI colors** of the editor — backgrounds, title bar, status bar, sidebar, selection highlights, and so on. It is a flat object whose keys are predefined VS Code interface names, for example:

| Key                         | Purpose                  |
| --------------------------- | ------------------------ |
| `editor.background`         | Editor background        |
| `editor.foreground`         | Default foreground color |
| `titleBar.activeBackground` | Title bar background     |
| `statusBar.background`      | Status bar background    |
| `sideBar.background`        | Sidebar background       |

> ⚠️ **The `type` field**: the top-level `type` field decides whether the theme is dark or light, and takes the value `"dark"` or `"light"`. It affects how VS Code renders its default controls (such as the adaptive colors of scrollbars and input boxes).

### `tokenColors`: code syntax highlighting

`tokenColors` is an **array of rules**. Each rule is made of a `scope` and a `settings`:

- `scope`: the TextMate scope(s) to match. It can be a string or an array of strings.
- `settings`: the colors and styles applied to that scope (`foreground`, `fontStyle`, `background`).

```json
{
  "name": "Comment",
  "scope": ["comment", "punctuation.definition.comment"],
  "settings": {
    "fontStyle": "italic",
    "foreground": "#a5b4cb"
  }
}
```

#### ⚠️ Important

the **order** of the `tokenColors` array matters — later rules override earlier rules with the same `scope`. This is also why, once the theme is engineered, a standalone `base.yaml` is used for shared rules, with language-specific rules layered on top.

---

## 4. How to Find the Right Key Names

This is the most common question every theme developer hits: "I want to make the status bar text blue — what key name should I use?"

VS Code provides two very powerful built-in tools that completely solve this problem.

### 4.1 UI colors: generate the current theme

Open the command palette with `Ctrl+Shift+P` and run **`Developer: Generate Color Theme From Current Settings`**.

This prints a JSON in the output panel containing **every UI color key the current theme uses**, along with their values. All you need to do is:

1. Find the area you care about in the output (for example, `statusBar.foreground`).
2. Copy that key name into your own `colors` object.
3. Change it to your own color value.

The command outputs the "currently effective values" — even if you never configured a key yourself (VS Code is using its default), it still appears in the output. In effect, this gives you a complete catalog of key names.

### 4.2 Syntax scopes: the Inspect tool

This is the **go-to tool** for debugging syntax highlighting. Open any code file, place the cursor on the element you want to color, and run **`Developer: Inspect Editor Tokens and Scopes`**.

The popup shows you:

- **The element's current TextMate scope chain**, from the most specific to the most general.
- **Which rule the current color comes from** (the `foreground` source), and which file defined it.

Usage tips:

- **Choose the most specific scope** to hit exactly the element you want, and avoid affecting sibling elements.
- For example, comments come in both `comment.line` and `comment.block`. If you only want to color line comments, pick `comment.line`; if you want to treat all comments uniformly, pick `comment`.
- When an element's color looks "wrong", the Inspect window tells you exactly which rule the current color comes from — a much clearer way to trace the problem.

---

## 5. Local Debugging

Once your theme file looks correct, the next step is to load it into VS Code and see it live.

### 5.1 Create package.json

To run a theme extension, you need a minimal `package.json` that declares it as an extension:

```json
{
  "name": "my-theme",
  "displayName": "My Theme",
  "version": "0.0.1",
  "publisher": "your-name",
  "engines": {
    "vscode": "^1.109.0"
  },
  "categories": ["Themes"],
  "contributes": {
    "themes": [
      {
        "label": "My Theme",
        "uiTheme": "vs-dark",
        "path": "./themes/my-theme.json"
      }
    ]
  }
}
```

Key fields:

- **`contributes.themes`**: declares the theme list. Each entry contains `label` (the name shown in the color theme picker), `uiTheme` (the base color scheme: `vs-dark` for dark / `vs` for light), and `path` (the relative path to the theme JSON).
- **`publisher`**: your publisher ID. You can use a placeholder first and change it after you register before publishing.
- **`engines`**: the minimum VS Code version requirement.

### 5.2 Press F5 to launch the Extension Development Host

1. Open the project folder in VS Code.
2. Press `F5` to launch the "Extension Development Host" window.
3. In the development host, press `Ctrl+K Ctrl+T` and select your theme.
4. Open a test code file to see the effect in real time.

After modifying the theme JSON, press `Ctrl+R` in the development host to reload — the change takes effect immediately. **Note**: after changing `contributes.themes` in `package.json`, you need to restart the development host session for it to take effect.

---

## 6. Preparing for Publishing

### 6.1 Complete package.json

Before publishing, fill in the key fields:

```json
{
  "name": "my-theme",
  "displayName": "My Theme",
  "description": "A short description of your theme",
  "version": "1.0.0",
  "publisher": "your-publisher-id",
  "engines": { "vscode": "^1.109.0" },
  "categories": ["Themes"],
  "icon": "images/icon.png",
  "repository": {
    "type": "git",
    "url": "https://github.com/yourname/my-theme"
  }
}
```

### 6.2 Prepare screenshots

Create an `images/` folder in the root and put in at least 3–5 code screenshots in different languages (1280×640 is recommended) for the README and store listing.

### 6.3 Write a README.md

Include the theme name, preview screenshots, design philosophy, installation instructions, and an optional color palette table.

### 6.4 Add a LICENSE

The MIT license is a good default — create a `LICENSE` file.

### 6.5 Create `.vscodeignore`

Exclude files that do not need to be packaged, **but make sure to keep the `images/` folder**:

```bash
.vscode/**
.gitignore
node_modules
```

---

## 7. Pre-Publishing Checklist

Before you run `vsce package`, spend two minutes going through this checklist to avoid the most common publishing mistakes:

- **`package.json` info**: make sure `publisher`, `name`, and `version` are correct (the `publisher` must exactly match the ID you registered with the marketplace).
- **Icon file**: confirm the `icon` path points to a **128×128 PNG image** and that the file actually exists at that location.
- **Preview images**: check that `README.md` includes at least one theme preview image (screenshots from the `images/` folder are recommended). A theme without preview images is unlikely to attract users.
- **`.vscodeignore` config**: confirm you have excluded unnecessary files, but **keep the `images/` folder** — otherwise the screenshots will not be packaged with the extension.
- **Local packaging test**: run `vsce package`. If a `.vsix` file is generated successfully, your configuration is mostly correct. If it fails, read the error carefully — the most common causes are a wrong `icon` path or a missing `publisher`.

---

## 8. Packaging and Publishing

### 8.1 Install the publishing tool

```bash
npm install -g @vscode/vsce
```

### 8.2 Test the package

```bash
vsce package
```

If successful, this generates a `.vsix` file. You can drag it into VS Code to install and test it manually. If it fails, read the error carefully — common causes are a wrong `icon` path or a `.vscodeignore` that excludes necessary files.

### 8.3 Get a Personal Access Token

- Sign in to [Azure DevOps](https://dev.azure.com) (with the Microsoft account you registered for the marketplace).
- Top-right avatar → Personal access tokens → New Token.
- Choose any name, select **All accessible organizations**, and set the expiration to 1 year (recommended).
- **Permissions**: only check **Marketplace → Manage**.
- **Copy the token immediately** after creating it (it is shown only once).

### 8.4 Sign in and publish

```bash
vsce login your-publisher-id
# Paste the token (it will not be displayed, just press Enter)

vsce publish
```

The theme will be uploaded within seconds, and you can search for it in VS Code after about 5–10 minutes.

#### Common publishing errors

- `Token verification failed`: permissions were not set correctly, or the token expired. Generate a new one.
- `Version already exists`: the version number already exists. Update the `version` field in `package.json`.
- Network issues: try a different network or use a proxy.

### 🔁 Alternative: upload the .vsix manually

If you run into trouble with the command-line approach, you can easily upload through the browser:

1. First make sure `vsce package` successfully generated a `.vsix` file.
2. Visit the [VS Code Marketplace management page](https://marketplace.visualstudio.com/manage) and sign in with your Microsoft account.
3. Click your publisher name to enter the publisher management view.
4. Click the **Publish extension** button in the top-right.
5. Select your `.vsix` file and upload it.
6. The theme will appear on the marketplace within a few minutes.

#### Advantages

it completely bypasses command-line token verification, and the whole process is visual.

---

## 9. After Publishing

- Check your marketplace page: `https://marketplace.visualstudio.com/items?itemName=your-publisher-id.your-theme-name`
- Sign in to the [marketplace management console](https://marketplace.visualstudio.com/manage) to view reports (page views, installs, conversion rate).
- Add README badges to your GitHub repository (such as version and download counts).
- Collect feedback and plan the next update.

---

## 10. Design Philosophy: From "Nice Looking" to "Easy to Read"

Many beginners think a theme is just a few nice colors. But a truly great theme lets the structure of your code surface by itself. This is the idea behind what Moongate calls **"visual depth"**:

- **Operators and punctuation should recede** (lower brightness), so they do not interfere with reading.
- **Function names should glow** (higher brightness), acting as visual anchors.
- **Readonly variables use italics** to hint at "immutability".
- **Deprecated code gets a strikethrough**, so you can spot it at a glance.

In Moongate, these principles are realized as "semantic layering": three visual levels — foreground (core logic), midground (ordinary code), and background (auxiliary information). You will see in later articles in this series how it grows into a full design system.

---

## Why Handwritten JSON Does Not Scale

By now you have a VS Code theme you can edit manually and publish. But once you really use it, you will quickly run into these pain points:

- A single JSON file can easily grow to thousands of lines. To change one color you have to search the whole file, and it is easy to edit the wrong one.
- To customize highlighting for each language, you end up piling more and more rules into one `tokenColors` array, which becomes hard to maintain.
- To try a light version, you have to copy the entire file and manually change hundreds of color values.
- One wrong scope, and your highlighting simply does not match — "the rule is written but never applies".

These problems are not your fault. They are the **limits of handwritten JSON as a workflow**. Solving them is exactly what the rest of this series is about — from engineering to design systems, making theme maintenance easy and elegant.

---

> **📎 Implementation Reference**
>
> This series is built upon the Moongate Theme project. You can explore the complete source code and install it directly:
> - **Code**: [github.com/yuelinghuashu/moongate-theme](https://github.com/yuelinghuashu/moongate-theme)
> - **Install**: [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=yuelinghuashu.moongate-theme)
