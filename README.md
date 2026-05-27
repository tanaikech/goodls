# goodls

<a name="top"></a>
[![MIT License](http://img.shields.io/badge/license-MIT-blue.svg?style=flat)](LICENCE)

![goodls](images/fig1.jpg)

<a name="overview"></a>

# Overview

**goodls** is a high-performance Command Line Interface (CLI) tool designed to effortlessly download shared files and entire folder structures from Google Drive.

Whether you are pulling a single shared document without authentication, performing a resumable download of a massive dataset, or concurrently extracting thousands of files from a shared folder using an API key, `goodls` handles the complex underlying Google Drive API mechanics so you don't have to.

<a name="description"></a>

# Core Features

### 1. Frictionless Anonymous Downloads

Download shared files directly via their URL **without any authorization or OAuth2 setup**. Large files (which normally require multi-step cookie/code verification in the browser) are handled automatically behind the scenes.

### 2. High-Speed Concurrent Folder Extraction ⚡️ _(Updated in v3.2.0+)_

Download entire shared folders while perfectly preserving their internal directory structure. Powered by Go's advanced Goroutine worker pools and strictly enforced channel semaphores, `goodls` now downloads multiple files in parallel, drastically reducing extraction time without overwhelming your network. _(Note: Requires a simple API key)._

### 3. Beautiful Multi-Progress UI 📊 _(New in v3.2.0+)_

Watch your data arrive in real-time. When downloading multiple files, `goodls` generates a clean, synchronized multi-bar interface directly in your terminal, showing live speeds, ETAs, and completion percentages for every active thread. Handles indeterminate file sizes (like Google Docs) with graceful spinners.

### 4. Bulletproof Resumable Downloads

Network drop? No problem. Run resumable downloads for massive files. You can specify exact byte chunks (e.g., `100m` for 100 Megabytes) to safely append data to an existing incomplete file.

### 5. Secure Credential Management 🔒 _(New in v3.2.0)_

Strict API key masking and source tracking ensure your credentials never accidentally leak into terminal logs or CI/CD pipelines, while still giving you precise feedback on exactly which key is driving the process.

---

# How to Install

### Option A: Download the Pre-compiled Binary (Easiest for Beginners)

You do not need to know how to code to use `goodls`. Simply download the executable file that matches your operating system from the [Releases Page](https://github.com/tanaikech/goodls/releases) and place it in a directory included in your system's `PATH`.

The following builds are available:

- `goodls_darwin_amd64` (macOS)
- `goodls_linux_386` (Linux 32-bit)
- `goodls_linux_amd64` (Linux 64-bit)
- `goodls_linux_armv7` (Linux ARM / Raspberry Pi)
- `goodls_linux_armv8` (Linux ARM64)
- `goodls_windows_386.exe` (Windows 32-bit)
- `goodls_windows_amd64.exe` (Windows 64-bit)

### Option B: Build from Source (For Go Developers)

If you have Go installed (Go 1.26+ recommended), you can compile and install it globally in one command:

```bash
$ go install github.com/tanaikech/goodls@latest
```

---

# Usage Guide

<a name="downloadsharedfiles"></a>

## 1. Basic Single File Download

You can use this command immediately after installing. **No API key or login is required.**

```bash
$ goodls -u [URL of shared file on Google Drive]
```

**Supported URLs:**

- Google Docs/Sheets/Slides: `https://docs.google.com/document/d/#####/edit?usp=sharing`
- Standard Drive Files: `https://drive.google.com/file/d/#####/view?usp=sharing`
- Web Content Links: `https://drive.google.com/uc?export=download&id=###`

**Common Options:**

- `-e [extension]`: Convert Google Docs to specific formats. (e.g., `-e pdf` or `-e ms` to convert to Microsoft Office formats like `.docx` / `.xlsx`).
- `-f [filename]`: Specify a custom name for the downloaded file.

#### Advanced: Download from a List of URLs

If you have a text file (`sample.txt`) containing multiple URLs, you can pipe it directly into `goodls` to process them all concurrently:

```bash
$ cat sample.txt | goodls
# or
$ goodls < sample.txt
```

<a name="downloadfilesfromfolder"></a>

## 2. Download Entire Shared Folders (Requires API Key)

To download an entire folder, you must provide a Google Cloud API Key.

```bash
$ goodls -u https://drive.google.com/drive/folders/#####?usp=sharing -key [Your_API_Key]
```

### ⚡️ Supercharge with Concurrency

By default, `goodls` will strictly limit concurrent downloads to 5 files at the same time to balance speed and stability. You can increase this limit to saturate your network bandwidth using the `-c` or `--concurrency` flag:

```bash
$ goodls -u [Folder_URL] -key [API_Key] -c 10
```

### Folder Download Options:

- `-m [mimeType]`: Filter downloads. E.g., `-m "application/pdf,image/png"` downloads _only_ PDFs and PNGs from the folder.
- `--overwrite` / `-o`: Overwrite local files if they already exist.
- `--skip` / `-s`: Skip downloading files that already exist locally.
- `--notcreatetopdirectory` / `-ntd`: Dump the folder's contents directly into your current working directory without wrapping them in the top-level folder name.
- `--skiperror` / `-se`: If one file fails, ignore it and continue downloading the rest of the folder.

<a name="retrieveapikey"></a>

### How to Retrieve an API Key (Beginner Tutorial)

1. Go to the [Google Cloud Console](https://console.cloud.google.com/).
2. Create a "New Project" and open it.
3. Open the left sidebar (hamburger menu) ➔ **APIs & Services** ➔ **Library**.
4. Search for **Google Drive API** and click **ENABLE**.
5. Go back to the left sidebar ➔ **APIs & Services** ➔ **Credentials**.
6. Click **Create Credentials** ➔ **API Key**.
7. Copy the generated key.

#### Keeping Your Key Safe (Environment Variable)

Instead of pasting your key into the command line every time, save it as an environment variable. `goodls` will automatically detect it:

```bash
# Add this to your ~/.bashrc or ~/.zshrc
export GOODLS_APIKEY="your_api_key_here"
```

#### Anonymous Mode Override

If you have `GOODLS_APIKEY` set in your environment but want to force `goodls` to run purely anonymously (ignoring the key), use the `--no-apikey` (`-nk`) flag:

```bash
$ goodls -u [URL] --no-apikey
```

<a name="resumabledownloadoffile"></a>

## 3. Resumable Download for Massive Files

If you are downloading a massive dataset (e.g., 50GB) and your network drops, you don't want to start over. Use the `-r` flag to download in chunks. _(Requires API Key)_.

```bash
$ goodls -u [URL] -key [API_Key] -r 100m
```

- `-r 100m`: Downloads exactly 100 Megabytes. If you run the command again, it will append the _next_ 100 Megabytes to the file automatically.
- `-r 1g`: Downloads in 1 Gigabyte chunks.

`goodls` verifies the exact byte size and MD5 checksums of your local file against Google Drive to ensure bit-perfect resume accuracy.

---

<a name="licence"></a>

# Licence

[MIT](LICENCE)

<a name="author"></a>

# Author

[Tanaike](https://tanaikech.github.io/about/)

If you have any questions and commissions for me, feel free to tell me.

<a name="updatehistory"></a>

# Update History

- **v3.2.2 (May 27, 2026)**
  1. **Critical Progress Bar Lifecycle Fix**: Fixed a bug where indeterminate file sizes (such as Google Docs exports returning `0` size from the Drive API) caused the progress bar spinners to hang infinitely. Bar completion is now strictly enforced (`SetTotal(-1, true)`) upon IO completion.
  2. **Resource Leak Patches**: Hardened HTTP body and local file descriptor management using strictly placed `defer` statements, preventing memory leaks during connection errors.

- **v3.2.1 (May 27, 2026)**
  1. **Strict Channel Semaphore**: Patched an edge case where standard `errgroup` limits were bypassed, causing folder downloads to dump excessive concurrent requests. The `--concurrency` (`-c`) limit is now strictly guaranteed at the language level using buffered channels.

- **v3.2.0 (May 27, 2026) - Massive Performance & Security Refactor**
  1. **Fully Concurrent Architecture (`-c`, `--concurrency`)**: Replaced sequential downloading with highly optimized Goroutine worker pools. Downloading entire folders or processing standard input lists is now exponentially faster.
  2. **Multi-Progress Bar UI (`github.com/vbauerster/mpb/v8`)**: Introduced a beautiful, real-time, synchronized terminal UI. Users can now visually track the exact transfer speed, ETA, and progress of multiple simultaneous downloads.
  3. **Extreme CPU Optimization**: Eliminated severe performance bottlenecks by replacing repeated dynamic `json.Unmarshal` calls with strictly typed O(1) static maps for MIME/Extension lookups.
  4. **Strict Security & Vulnerability Patches**: Enforced modern dependency resolution in `go.mod` to permanently patch Dependabot CVE alerts (including gRPC-Go authorization bypass and `x/crypto/ssh` memory panics). Upgraded CLI parser to `urfave/cli/v2`.
  5. **Advanced API Key Tracking**: Keys are now heavily masked in terminal output (`AIza****`). Output routes strictly to `os.Stderr` to protect users' JSON pipeline automations.
  6. **Anonymous Override Mode (`--no-apikey`, `-nk`)**: Added a flag to explicitly ignore environment variables and force unauthenticated API requests for safe testing.

- v2.0.6 (June 13, 2025)
  1. Rebuild by go1.24.4.

- v2.0.5 (March 10, 2023)
  1. From this version, when the API key is used, the large file is downloaded by the API key. Because the specification for downloading the shared large file is sometimes changed. When the API key is not used, the shared large file is downloaded by the current specification (v2.0.4).

- v2.0.4 (March 9, 2023)
  1. From January 2024, it seems that the specification of the process for downloading a large shared file on Google Drive has been changed. So I updated goodls to reflect this. The usage of goodls has not changed.

- v2.0.3 (April 5, 2023)
  1. Forgot to update the version number and modified it. And, built the sources with the latest version.

- v2.0.2 (February 24, 2023)
  1. Modified go.mod and go.sum.

- v2.0.1 (February 26, 2022)
  1. A bug for the resumable download was removed.

- v2.0.0 (February 25, 2022)
  1. By changing the specification of methods, `drive.New()` and `transport.APIKey` were deprecated. By this, I updated go-getfilelist. In this version, I used this updated library to goodls. And also, `drive.NewService()` is used instead of `drive.New()`.

- v1.2.8 (February 17, 2022)
  1. Recently, it seems that the specification the process for downloading the shared file on Google Drive has been changed. So I updated goodls for reflecting this. The usage of goodls is not changed.

- v1.2.7 (August 21, 2020)
  1. As the URL for downloading the files, `webContentLink` was added. So from this version, the URL of `https://drive.google.com/uc?export=download&id=###` got to be able to be used.

- v1.2.6 (February 23, 2020)
  1. Added `--skiperror` flag. When the files are downloaded from the shared folder, if an error occurs, the download was stopped. This skips the error and continues.

- v1.2.5 (January 29, 2020)
  1. Added `--notcreatetopdirectory` (`-ntd`). When this option is used, the top directory is not created and all files and sub-folders under the top folder are downloaded under the working directory.

- v1.2.4 (January 3, 2020)
  1. Fixed compilation error caused by updates in `github.com/urfave/cli`.

- v1.2.3 (October 31, 2019)
  1. Added `-d` directory flag for saving downloaded files to a specific location.

- v1.2.2 (December 12, 2018)
  1. Added `-m` flag. When files are downloaded from a specific folder, it got to be able to select mimeType.

- v1.2.1 (November 25, 2018)
  1. API key got to be able to be used by an environment variable (`GOODLS_APIKEY`).

- v1.2.0 (November 24, 2018)
  1. Resumable download capability added for large shared files using API key.

- v1.1.1 (November 13, 2018)
  1. Version of `go-getfilelist` was updated.

- v1.1.0 (November 4, 2018)
  1. Files from the shared folder got to be able to be downloaded while keeping the folder structure using API key.
  2. Information of shared file and folder can be retrieved.
  3. Added Google Docs to Microsoft Office conversion via `-e ms`.

- v1.0.3 (September 4, 2018)
  1. Download progress display added.
  2. `--np` option added to suppress progress display.

- v1.0.2 (May 10, 2018)
  1. Support for files with large size (chunked saving).

- v1.0.1 (January 11, 2018)
  1. Support for multiple URLs using Standard Input and Pipe.

- v1.0.0 (January 10, 2018)
  1. Initial release.

[TOP](#top)
