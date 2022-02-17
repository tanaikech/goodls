// Package main (resumableDownload.go) :
// These methods are for resumable downloading a shared file from Google Drive.
package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
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
	"google.golang.org/api/googleapi/transport"
)

// valResumableDownload : Structure for resumable download
type valResumableDownload struct {
	para
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

// getFileInfFromP : Retrieve file information from *para.
func (p *para) getFileInfFromP() (*drive.File, error) {
	v := &valResumableDownload{
		para: *p,
	}
	v.Client = &http.Client{
		Transport: &transport.APIKey{Key: p.APIKey},
	}
	if err := v.getFileInf(); err != nil {
		return nil, err
	}
	return v.DownloadFile, nil
}

// showFileInf : Show file information.
func (p *para) showFileInf() error {
	dlfile, err := p.getFileInfFromP()
	if err != nil {
		return err
	}
	r, err := json.Marshal(dlfile)
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", r)
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

// resDownloadFileByAPIKey : Resumagle download by API key.
func (v *valResumableDownload) resDownloadFileByAPIKey() (*http.Response, error) {
	u, err := url.Parse(driveAPI)
	if err != nil {
		return nil, err
	}
	u.Path = path.Join(u.Path, v.DownloadFile.Id)
	q := u.Query()
	q.Set("alt", "media")
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
	v.Client.Timeout = time.Duration(timeOut) * time.Second
	req, err := http.NewRequest("get", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", v.Range)
	res, err := v.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 206 && res.StatusCode != 200 {
		r, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		return nil, fmt.Errorf("%s", r)
	}
	return res, nil
}

// getFileInf : Retrieve file infomation using Drive API.
func (v *valResumableDownload) getFileInf() error {
	srv, err := drive.New(v.Client)
	if err != nil {
		return err
	}
	fields := []googleapi.Field{"createdTime,id,md5Checksum,mimeType,modifiedTime,name,owners,parents,shared,size,webContentLink,webViewLink"}
	res, err := srv.Files.Get(v.ID).Fields(fields...).Do()
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
// st is 2 dimensional array including values.
// k is the index of each element for setting indent.
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
			[]string{"Current status", "New download"},
			[]string{"Save filename", v.Filename},
			[]string{"Filename in Google Drive", v.DownloadFile.Name},
			[]string{"Current file size of local [bytes]", strconv.FormatInt(v.CurrentFileSize, 10)},
			[]string{"File size of Google Drive [bytes]", strconv.FormatInt(v.DownloadFile.Size, 10)},
			[]string{"This download size [bytes]", strconv.FormatInt(v.Size, 10)},
		}
		return getMsg(setIndent(st, 0), " : ")
	case fc && !end:
		st := [][]string{
			[]string{"Current status", "Resumable download"},
			[]string{"Save filename", v.Filename},
			[]string{"Filename in Google Drive", v.DownloadFile.Name},
			[]string{"Current file size of local [bytes]", strconv.FormatInt(v.CurrentFileSize, 10)},
			[]string{"File size of Google Drive [bytes]", strconv.FormatInt(v.DownloadFile.Size, 10)},
			[]string{"This download size [bytes]", strconv.FormatInt(v.Size, 10)},
		}
		return getMsg(setIndent(st, 0), " : ")
	case !fc && end:
		cs, err := getMd5Checksum(filepath.Join(v.WorkDir, v.Filename))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		st := [][]string{
			[]string{"Current status", "Download has already done."},
			[]string{"Save filename", v.Filename},
			[]string{"Filename in Google Drive", v.DownloadFile.Name},
			[]string{"Current file size of local [bytes]", strconv.FormatInt(v.CurrentFileSize, 10)},
			[]string{"File size of Google Drive [bytes]", strconv.FormatInt(v.DownloadFile.Size, 10)},
			[]string{"md5checksum at Google Drive", v.DownloadFile.Md5Checksum},
			[]string{"md5checksum at Local Drive", cs},
		}
		return getMsg(setIndent(st, 0), " : ")
	default:
		return fmt.Sprintf("unknown error")
	}
}

// resumableDownload : Main method of resumable download.
func (p *para) resumableDownload() error {
	p.Client = &http.Client{
		Transport: &transport.APIKey{Key: p.APIKey},
	}
	v := &valResumableDownload{
		para: *p,
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
			return v.para.saveFile(res)
		}
	} else {
		fmt.Printf("\n%s\n", msg)
	}
	return nil
}
