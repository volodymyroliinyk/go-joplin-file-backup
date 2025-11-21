# go-joplin-file-backup

A lightweight, reliable Go-based tool that automatically backs up local files into **Joplin Desktop** using the **Web
Clipper API**.
Each file becomes a dedicated Joplin note with the file attached as a resource.
On subsequent runs, the note is **updated**, the attachment is **replaced**, and unused old resources are **safely
cleaned up**.

---

## Features

* Scans any directory recursively.
* Processes files by a given extension (e.g., `.smmx`, `.pdf`, `.json`, etc.).
* For each file:

    * Extracts a stable **creation timestamp** (earliest of atime/mtime/ctime).
    * Uploads the file as a Joplin **resource**.
    * Creates or updates a note inside the specified notebook.
    * Stores metadata in note body:

        * `created_at` – original file creation timestamp
        * `upload_at` – when the file was backed up into Joplin
        * `file_path` – full path to the original file
* Cleans up **old unused Joplin resources** after updating a note.
* Never deletes notes, notebooks, or tags.
* Ideal for automated offline backups of sensitive or important files.

---

## Requirements

* **Joplin Desktop** installed.
* **Web Clipper API enabled**:

    1. Joplin → Tools → Options → Web Clipper
    2. Enable the service
    3. Copy your API token
* Go **1.25+**

---

## Configuration

Set your Joplin Web Clipper token:

```bash
export JOPLIN_TOKEN="your-joplin-token"
```

By default, the script uses:

```
http://127.0.0.1:41184
```

Make sure Web Clipper is running.

---

## Usage

```bash
./go run main.go --notebook_id="<notebook_id>" --directory="/path/to/files" --file_extension=".smmx"
```

### Parameters

| Flag               | Description                                                           |
|--------------------|-----------------------------------------------------------------------|
| `--notebook_id`    | The target Joplin notebook ID where notes will be created or updated. |
| `--directory`      | Directory to scan for files. Scanned recursively.                     |
| `--file_extension` | Filter by file extension (default: `.smmx`).                          |

---

## How It Works

### 1. Scanning files

The tool walks the directory recursively and finds all files matching the given extension.

### 2. Metadata extraction

The script determines the earliest timestamp from:

* `mtime`
* `atime`
* `ctime`

This ensures stable "true" creation dates even across filesystems.

### 3. Creating/updating notes

Each file corresponds to a Joplin note titled exactly as the filename:

```
map1.smmx
```

The note body looks like:

```
created_at: "2023-10-12 11:22:03.000 -0400"
upload_at:  "2024-01-15 20:10:55.512 -0500"
file_path:  "/home/user/mindmaps/map1.smmx"

[map1.smmx](:/RESOURCE_ID)
```

### 4. Resource cleanup

When a file changes:

* A **new resource** is uploaded.
* The note is updated to reference the new resource.
* Old unused resources are **deleted** to prevent storage bloat.

Notes, notebooks, and tags are never removed.

---

## Safety Notes

* This tool **never deletes**:

    * notes
    * notebooks
    * tags
* Only unused *resources* of updated notes are deleted.
* If the same resource is reused in another note (edge case), it will not be deleted.

---

## Use Cases

* Backup of mind map files (`.smmx`)
* Backup of diagrams, research documents, JSON exports, etc
* Personal digital archiving inside Joplin
