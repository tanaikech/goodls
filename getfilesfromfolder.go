// Package main (getfilesfromfolder.go) :
// These methods are for downloading all files from a shared folder of Google Drive.
// Refactored to support robust concurrent downloads using strict channel semaphores.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	getfilelist "github.com/tanaikech/go-getfilelist"
	"golang.org/x/sync/errgroup"
	drive "google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

const (
	driveAPI = "https://www.googleapis.com/drive/v3/files"
)

// mime2ext : Convert mimeType to extension directly from map (O(1)).
func mime2ext(mime string) string {
	return mimeVsEx[mime]
}

// downloadFileByAPIKey : Download file using API key.
func (p *para) downloadFileByAPIKey(file *drive.File) error {
	u, err := url.Parse(driveAPI)
	if err != nil {
		return err
	}
	u.Path = path.Join(u.Path, file.Id)
	q := u.Query()
	q.Set("key", p.APIKey)
	if strings.Contains(file.MimeType, "application/vnd.google-apps") {
		u.Path = path.Join(u.Path, "export")
		q.Set("mimeType", file.WebViewLink)
	} else {
		q.Set("alt", "media")
		q.Set("supportsAllDrives", "true")
	}
	u.RawQuery = q.Encode()

	p.WorkDir = file.WebContentLink
	p.Filename = file.Name

	timeOut := func(size int64) int64 {
		if size == 0 {
			switch {
			case size < 100000000:
				return 3600
			case size > 100000000:
				return 0
			}
		}
		return 0
	}(file.Size)
	p.Client = &http.Client{
		Timeout: time.Duration(timeOut) * time.Second,
	}

	res, err := p.fetch(u.String())
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		r, err := io.ReadAll(res.Body)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if p.SkipError {
			fmt.Printf("!! Downloading '%s' (fileId: %s) was skipped by an error. Status code is %d.\n", file.Name, file.Id, res.StatusCode)
			return nil
		}
		return fmt.Errorf("%s", r)
	}
	return p.saveFile(res)
}

// makeFileByCondition : Make file by condition.
func (p *para) makeFileByCondition(file *drive.File) error {
	targetPath := filepath.Join(file.WebContentLink, file.Name)

	var remoteTime time.Time
	if file.ModifiedTime != "" {
		if t, err := time.Parse(time.RFC3339, file.ModifiedTime); err == nil {
			remoteTime = t
		}
	}

	resolvedPath, action, err := p.resolveConflict(targetPath, remoteTime)
	if err != nil {
		return err
	}

	if action == "skip" {
		if !p.Disp {
			p.mu.Lock()
			fmt.Fprintf(os.Stderr, "[*] Skipped: '%s' already exists.\n", filepath.Base(targetPath))
			p.mu.Unlock()
		}
		return nil
	}

	file.Name = filepath.Base(resolvedPath)
	file.WebContentLink = filepath.Dir(resolvedPath)
	p.ConflictResolved = true

	return p.downloadFileByAPIKey(file)
}

// makeDir : Make a directory by checking duplication.
func (p *para) makeDir(folder string) error {
	if err := os.MkdirAll(folder, 0777); err != nil {
		return err
	}
	return nil
}

// chkFile : Check the existence of file and directory in local PC.
func chkFile(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

// makeDirByCondition : Make directory by condition.
func (p *para) makeDirByCondition(dir string) error {
	info, err := os.Stat(dir)
	if err == nil {
		if info.IsDir() {
			return nil
		}
		return fmt.Errorf("cannot create directory '%s', a file with that name already exists", dir)
	}
	return p.makeDir(dir)
}

// initDownload : Download files concurrently by Drive API using API key.
func (p *para) initDownload(fileList *getfilelist.FileListDl) error {
	if !p.Disp {
		fmt.Printf("Download files from a folder '%s'.\n", fileList.SearchedFolder.Name)
		fmt.Printf("There are %d files and %d folders in the folder.\n", fileList.TotalNumberOfFiles, fileList.TotalNumberOfFolders-1)
		fmt.Println("Starting download.")
	}

	idToName := map[string]interface{}{}
	for i, e := range fileList.FolderTree.Folders {
		idToName[e] = fileList.FolderTree.Names[i]
	}

	type downloadJob struct {
		file *drive.File
		path string
	}
	var jobs []downloadJob

	// Create directories sequentially to prevent race conditions.
	for _, e := range fileList.FileList {
		targetPath := p.WorkDir
		if p.Notcreatetopdirectory {
			e.FolderTree = append(e.FolderTree[:0], e.FolderTree[1:]...)
		}
		for _, dir := range e.FolderTree {
			targetPath = filepath.Join(targetPath, idToName[dir].(string))
		}
		if targetPath != p.WorkDir {
			if err := p.makeDirByCondition(targetPath); err != nil {
				return err
			}
		}

		for _, file := range e.Files {
			if file.MimeType != "application/vnd.google-apps.script" {
				jobs = append(jobs, downloadJob{file: file, path: targetPath})
			} else {
				if !p.Disp {
					fmt.Printf("'%s' is a project file. Project file cannot be downloaded using API key.\n", file.Name)
				}
			}
		}
	}

	// Use strict channel semaphore to guarantee concurrency limit across environments
	eg, _ := errgroup.WithContext(context.Background())
	sem := make(chan struct{}, p.Concurrency)

	for _, job := range jobs {
		job := job
		eg.Go(func() error {
			sem <- struct{}{}
			defer func() { <-sem }()

			workerP := p.Clone()
			job.file.WebContentLink = job.path
			workerP.Size = job.file.Size
			return workerP.makeFileByCondition(job.file)
		})
	}

	return eg.Wait()
}

// defFormat : Default download format directly from map.
func defFormat(mime string) string {
	return defaultformat[mime]
}

// extToMime : Convert from extension to mimeType of the file on Local directly from map.
func extToMime(ext string) string {
	return extVsmime[strings.Replace(strings.ToLower(ext), ".", "", 1)]
}

// dupChkFoldersFiles : Check duplication of folder names and filenames.
func (p *para) dupChkFoldersFiles(fileList *getfilelist.FileListDl) {
	dupChk1 := map[string]bool{}
	cnt1 := 2
	for i, folderName := range fileList.FolderTree.Names {
		if !dupChk1[folderName] {
			dupChk1[folderName] = true
		} else {
			fileList.FolderTree.Names[i] = folderName + "_" + strconv.Itoa(cnt1)
		}
	}
	extt := strings.ToLower(p.Ext)
	for i, list := range fileList.FileList {
		if len(list.Files) > 0 {
			dupChk2 := map[string]bool{}
			cnt2 := 2
			for j, file := range list.Files {
				if !dupChk2[file.Name] {
					dupChk2[file.Name] = true
				} else {
					ext := filepath.Ext(file.Name)
					if ext != "" {
						fileList.FileList[i].Files[j].Name = file.Name[0:len(file.Name)-len(ext)] + "_" + strconv.Itoa(cnt2) + ext
					} else {
						fileList.FileList[i].Files[j].Name = file.Name + "_" + strconv.Itoa(cnt2)
					}
					cnt2++
				}
				mime := defFormat(file.MimeType)
				if extt != "" {
					if mime != "" {
						cmime := func() string {
							if (extt == "txt" || extt == "text") && file.MimeType == "application/vnd.google-apps.spreadsheet" {
								return extToMime("csv")
							} else if extt == "zip" && file.MimeType == "application/vnd.google-apps.presentation" {
								return extToMime("pptx")
							}
							return extToMime(extt)
						}()
						if cmime != "" {
							fileList.FileList[i].Files[j].WebViewLink = cmime
						} else {
							fileList.FileList[i].Files[j].WebViewLink = mime
						}
					}
				} else {
					fileList.FileList[i].Files[j].WebViewLink = mime
				}
				if file.MimeType != "application/vnd.google-apps.script" {
					ext := filepath.Ext(file.Name)
					if ext == "" {
						fileList.FileList[i].Files[j].Name += mime2ext(fileList.FileList[i].Files[j].WebViewLink)
					}
				}
			}
		}
	}
}

// getFilesFromFolder: This method is the main method for downloading all files in a shared folder.
func (p *para) getFilesFromFolder() error {
	srv, err := drive.NewService(context.Background(), option.WithAPIKey(p.APIKey))
	if err != nil {
		return err
	}
	fileList, err := func() (*getfilelist.FileListDl, error) {
		if len(p.InputtedMimeType) > 0 {
			return getfilelist.Folder(p.SearchID).MimeType(p.InputtedMimeType).Do(srv)
		}
		return getfilelist.Folder(p.SearchID).Do(srv)
	}()
	if err != nil {
		return err
	}
	if p.ShowFileInf {
		r, err := json.Marshal(fileList)
		if err != nil {
			return err
		}
		fmt.Printf("%s\n", r)
		return nil
	}
	p.dupChkFoldersFiles(fileList)
	if err := p.initDownload(fileList); err != nil {
		return err
	}
	return nil
}
