package goodls

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	drive "google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

// valResumableDownload : Structure for resumable download
type valResumableDownload struct {
	Para
	dlParams
}

// dlParams : Parameters for downloading
type dlParams struct {
	CurrentFileSize int64
	DownloadFile    *drive.File
	OutMimeType     string
	Range           string
	Start           int64
	End             int64
}

// getFileInfFromP : Retrieve file information from *Para.
func (p *Para) getFileInfFromP() (*drive.File, error) {
	v := &valResumableDownload{
		Para: *p,
	}
	v.Client = p.getHTTPClient()
	if err := v.getFileInf(); err != nil {
		return nil, err
	}
	return v.DownloadFile, nil
}

// showFileInf : Show file information.
func (p *Para) showFileInf() error {
	dlfile, err := p.getFileInfFromP()
	if err != nil {
		return err
	}
	r, err := json.Marshal(dlfile)
	if err != nil {
		return err
	}
	if !p.MCPMode {
		fmt.Printf("%s\n", r)
	}
	return nil
}

// getDownloadBytes : Get download size for resumable download.
func getDownloadBytes(size string) (int64, error) {
	reg := regexp.MustCompile("^([0-9.]+)$|^([0-9.]+)([bkmgt])")
	regRes := reg.FindStringSubmatch(strings.ToLower(size))
	switch {
	case len(regRes) == 0:
		return 0, fmt.Errorf("wrong size: %s", size)
	case regRes[1] != "":
		s, err := strconv.ParseInt(regRes[1], 10, 64)
		if err != nil {
			return 0, err
		}
		return s, nil
	case regRes[2] != "" && regRes[3] != "":
		f, err := strconv.ParseFloat(regRes[2], 64)
		if err != nil {
			return 0, err
		}
		switch regRes[3] {
		case "k":
			f *= 1000
		case "m":
			f *= 1000000
		case "g":
			f *= 1000000000
		case "t":
			f *= 1000000000000
		}
		s := int64(f)
		if s < 10000000 {
			return 10000000, nil
		}
		return s, nil
	default:
		return 0, fmt.Errorf("unexpected error '%s'", size)
	}
}

// resDownloadFileByAPIKey : Resumable download by API key.
func (v *valResumableDownload) resDownloadFileByAPIKey() (*http.Response, error) {
	u, err := url.Parse(driveAPI)
	if err != nil {
		return nil, err
	}
	u.Path = path.Join(u.Path, v.DownloadFile.Id)
	q := u.Query()
	q.Set("alt", "media")
	q.Set("key", v.APIKey)
	q.Set("supportsAllDrives", "true") // Added Shared Drive Support Explicitly
	u.RawQuery = q.Encode()
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
	}(v.DownloadFile.Size)

	v.Client = v.Para.getHTTPClient()
	v.Client.Timeout = time.Duration(timeOut) * time.Second

	var res *http.Response
	maxRetries := v.Retry
	if maxRetries < 0 {
		maxRetries = 0
	}

	for i := 0; i <= maxRetries; i++ {
		req, reqErr := http.NewRequest("GET", u.String(), nil)
		if reqErr != nil {
			return nil, reqErr
		}
		req.Header.Set("Range", v.Range)

		if v.Verbose {
			v.mu.Lock()
			fmt.Fprintf(os.Stderr, "[Verbose] Resumable fetching URL: %s (Attempt %d/%d)\n", u.String(), i+1, maxRetries+1)
			v.mu.Unlock()
		}

		res, err = v.Client.Do(req)

		if err == nil {
			if res.StatusCode == 429 || res.StatusCode >= 500 {
				if v.Verbose {
					v.mu.Lock()
					fmt.Fprintf(os.Stderr, "[Verbose] HTTP %d received for %s\n", res.StatusCode, u.String())
					v.mu.Unlock()
				}
				res.Body.Close()
				err = fmt.Errorf("HTTP %d", res.StatusCode)
			} else if res.StatusCode != 206 && res.StatusCode != 200 {
				r, _ := io.ReadAll(res.Body)
				res.Body.Close()
				return nil, fmt.Errorf("%s", r)
			} else {
				return res, nil
			}
		} else {
			if v.Verbose {
				v.mu.Lock()
				fmt.Fprintf(os.Stderr, "[Verbose] Error resumable fetching %s: %v\n", u.String(), err)
				v.mu.Unlock()
			}
		}

		if i < maxRetries {
			delay := time.Duration(v.RetryDelay) * time.Second * time.Duration(1<<i)
			if v.Verbose {
				v.mu.Lock()
				fmt.Fprintf(os.Stderr, "[Verbose] Waiting %v before retry...\n", delay)
				v.mu.Unlock()
			}
			time.Sleep(delay)
		}
	}

	return nil, fmt.Errorf("failed resumable fetch after %d retries: %v", maxRetries, err)
}

// getFileInf : Retrieve file infomation using Drive API.
func (v *valResumableDownload) getFileInf() error {
	opts := []option.ClientOption{option.WithAPIKey(v.Para.APIKey)}
	if v.Para.Proxy != "" {
		opts = append(opts, option.WithHTTPClient(v.Para.getHTTPClient()))
	}
	srv, err := drive.NewService(context.Background(), opts...)
	if err != nil {
		return err
	}
	fields := []googleapi.Field{"createdTime,id,md5Checksum,mimeType,modifiedTime,name,owners,parents,shared,size,webContentLink,webViewLink"}
	res, err := srv.Files.Get(v.ID).Fields(fields...).SupportsAllDrives(true).Do()
	if err != nil {
		return err
	}
	v.DownloadFile = res
	return nil
}

// chkResumeFile : Check file and file size of local file.
func (v *valResumableDownload) chkResumeFile() (bool, bool, error) {
	if v.Filename == "" {
		v.Filename = v.DownloadFile.Name
	}
	f, err := os.Stat(filepath.Join(v.WorkDir, v.Filename))
	if err != nil {
		v.CurrentFileSize = 0
		v.Start = 0
		v.End = func() int64 {
			if v.DownloadBytes >= v.DownloadFile.Size {
				return v.DownloadFile.Size - 1
			}
			return v.DownloadBytes - 1
		}()
		v.Range = fmt.Sprintf("bytes=0-%d", v.End)
		v.Size = v.End + 1
		return false, false, nil
	}
	fs := f.Size()
	v.CurrentFileSize = fs
	if fs == v.DownloadFile.Size {
		return false, true, nil
	} else if fs > v.DownloadFile.Size {
		return false, false, fmt.Errorf("size of download file is larger than that of local file. Please confirm the file and URL. FileName is %s. Download URL is %s", v.Filename, v.URL)
	}
	v.Start = fs
	v.End = func() int64 {
		if fs+v.DownloadBytes >= v.DownloadFile.Size {
			return v.DownloadFile.Size - 1
		}
		return fs + v.DownloadBytes - 1
	}()
	v.Range = fmt.Sprintf("bytes=%d-%d", v.Start, v.End)
	v.Size = v.End - v.Start + 1
	return true, false, nil
}

// setIndent : Set indent of each element using the maximum length of element.
func setIndent(st [][]string, k int) [][]string {
	maxLen := func(max int) int {
		for _, e := range st {
			if len(e[k]) > max {
				max = len(e[k])
			}
		}
		return max
	}(0)
	for i, e := range st {
		spaces := func(l int) string {
			temp := make([]string, l)
			for i := range temp {
				temp[i] = " "
			}
			return strings.Join(temp[:], "")
		}(maxLen - len(e[k]))
		st[i][k] = e[k] + spaces
	}
	return st
}

// getMsg : Convert 2D array to string using delimiter.
func getMsg(st [][]string, delim string) string {
	var temp []string
	for _, e := range st {
		temp = append(temp, strings.Join(e, delim))
	}
	return strings.Join(temp, "\n")
}

// getMd5Checksum : Get md5checksum
func getMd5Checksum(fileName string) (string, error) {
	f, err := os.Open(fileName)
	if err != nil {
		return "", err
	}
	defer f.Close()
	ha := md5.New()
	if _, err := io.Copy(ha, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(ha.Sum(nil)), nil
}

// getStatusMsg : Get status message.
func (v *valResumableDownload) getStatusMsg(fc, end bool) string {
	switch {
	case !fc && !end:
		st := [][]string{
			{"Current status", "New download"},
			{"Save filename", v.Filename},
			{"Filename in Google Drive", v.DownloadFile.Name},
			{"Current file size of local [bytes]", strconv.FormatInt(v.CurrentFileSize, 10)},
			{"File size of Google Drive [bytes]", strconv.FormatInt(v.DownloadFile.Size, 10)},
			{"This download size [bytes]", strconv.FormatInt(v.Size, 10)},
		}
		return getMsg(setIndent(st, 0), " : ")
	case fc && !end:
		st := [][]string{
			{"Current status", "Resumable download"},
			{"Save filename", v.Filename},
			{"Filename in Google Drive", v.DownloadFile.Name},
			{"Current file size of local [bytes]", strconv.FormatInt(v.CurrentFileSize, 10)},
			{"File size of Google Drive [bytes]", strconv.FormatInt(v.DownloadFile.Size, 10)},
			{"This download size [bytes]", strconv.FormatInt(v.Size, 10)},
		}
		return getMsg(setIndent(st, 0), " : ")
	case !fc && end:
		cs, err := getMd5Checksum(filepath.Join(v.WorkDir, v.Filename))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		st := [][]string{
			{"Current status", "Download has already done."},
			{"Save filename", v.Filename},
			{"Filename in Google Drive", v.DownloadFile.Name},
			{"Current file size of local [bytes]", strconv.FormatInt(v.CurrentFileSize, 10)},
			{"File size of Google Drive [bytes]", strconv.FormatInt(v.DownloadFile.Size, 10)},
			{"md5checksum at Google Drive", v.DownloadFile.Md5Checksum},
			{"md5checksum at Local Drive", cs},
		}
		return getMsg(setIndent(st, 0), " : ")
	default:
		return fmt.Sprintln("unknown error")
	}
}

// resumableDownload : Main method of resumable download.
func (p *Para) resumableDownload() error {
	v := &valResumableDownload{
		Para: *p,
	}
	if err := v.getFileInf(); err != nil {
		return err
	}
	if strings.Contains(v.DownloadFile.MimeType, "application/vnd.google-apps") {
		return fmt.Errorf("a Google Docs file cannot be resumable downloaded")
	}
	fc, end, err := v.chkResumeFile()
	if err != nil {
		return err
	}

	if p.MCPMode {
		if (!fc && !end) || (fc && !end) {
			res, err := v.resDownloadFileByAPIKey()
			if err != nil {
				return err
			}
			return v.Para.saveFile(res)
		}
		return nil
	}

	msg := v.getStatusMsg(fc, end)
	if (!fc && !end) || (fc && !end) {
		fmt.Printf("\n%s\n\n", msg)
		var input string
		fmt.Printf("Do you start this download? [y or n] ... ")
		if _, err := fmt.Scan(&input); err != nil {
			return err
		}
		if input == "y" {
			res, err := v.resDownloadFileByAPIKey()
			if err != nil {
				return err
			}
			return v.Para.saveFile(res)
		}
	} else {
		fmt.Printf("\n%s\n", msg)
	}
	return nil
}
