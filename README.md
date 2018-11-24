goodls
=====

<a name="TOP"></a>
[![Build Status](https://travis-ci.org/tanaikech/goodls.svg?branch=master)](https://travis-ci.org/tanaikech/goodls)
[![MIT License](http://img.shields.io/badge/license-MIT-blue.svg?style=flat)](LICENCE)

<a name="Overview"></a>
# Overview
This is a CLI tool to download shared files and folders from Google Drive. For large file, the resumable download can be also run.

<a name="Description"></a>
# Methods
### 1. [Download shared files from the shared URL without the authorization.](#downloadsharedfiles)
We have already known that the shared files on Google Drive can be downloaded without the authorization. But when the size of file becomes large (about 40MB), it requires a little ingenuity to download the file. It requires to access 2 times to Google Drive. At 1st access, it retrieves a cookie and a code for downloading. At 2nd access, the file is downloaded using the cookie and code. I created this process as a CLI tool.

### 2. [Download all shared files with the folder structure from the shared folder.](#downloadfilesfromfolder)
**This method uses API key.**

There are sometimes the situation for downloading files in a sharead folder. But I couldn't find the CLI applications for downloading files in the shared folder. So I implemented this. But when in order to retrieve the file list from the shared file, Drive API is required to be used. In order to use Drive API, it is required to use OAuth2, Service account and API key. So I selected to use API key which is the simplest way. This CLI tool can retrieve the file list in the shared folder using API key and download all files in the shared folder.

### 3. [Run resumable download for large files.](#resumabledownloadoffile)
**This method uses API key.**

At [a recent proposal](https://github.com/tanaikech/goodls/issues/3), I knew the requirement of the resumable download of shared file. So I implemented this.

# How to Install
Download an executable file of goodls from [the release page](https://github.com/tanaikech/goodls/releases) and import to a directory with path.

or

Use go get.

~~~bash
$ go get -u github.com/tanaikech/goodls
~~~

# Usage
<a name="downloadsharedfiles"></a>
## 1. Download shared files
<a name="demo1"></a>
![](images/demo1.gif)

The image used for this demonstration was created by [k3-studio](https://k3-studio.deviantart.com/art/Chromatic-spiral-416032436)

<a name="demo2"></a>
![](images/demo2.gif)

You can use this just after you download or install goodls. You are not required to do like OAuth2 process.

~~~bash
$ goodls -u [URL of shared file on Google Drive]
~~~

- **Help**
    - ``$ goodls --help``
- **Options**
    - ``-e``
        - Extension of output file. This is for only Google Docs (Spreadsheet, Document, Presentation). Default is ``pdf``. When ``ms`` is used, the sharead Googld Docs can be downloaded as Microsoft Docs.
        - Sample :
            - ``$ goodls -u https://docs.google.com/document/d/#####/edit?usp=sharing -e txt``
    - ``-f``
        - Filename of file which is output. When this was not used, the original filename on Google Drive is used.
        - Sample :
            - ``$ goodls -u https://docs.google.com/document/d/#####/edit?usp=sharing -e txt -f sample.txt``
- **URL is like below.**
    - In the case of Google Docs (Spreadsheet, Document, Slides)
        - ``https://docs.google.com/spreadsheets/d/#####/edit?usp=sharing``
        - ``https://docs.google.com/document/d/#####/edit?usp=sharing``
        - ``https://docs.google.com/presentation/d/#####/edit?usp=sharing``
    - In the case of except for Google Docs
        - ``https://drive.google.com/file/d/#####/view?usp=sharing``


### File with several URLs
If you have a file including URLs, you can input the URL data using standard input and pipe as follows. If wrong URL is included, the URL is skipped.

~~~bash
$ cat sample.txt | goodls
~~~

or

~~~bash
$ goodls < sample.txt
~~~

**sample.txt**

~~~
https://docs.google.com/spreadsheets/d/#####/edit?usp=sharing
https://docs.google.com/document/d/#####/edit?usp=sharing
https://docs.google.com/presentation/d/#####/edit?usp=sharing
~~~

**When you download shared files from Google Drive, please confirm whether the files are shared.**

<a name="downloadfilesfromfolder"></a>
## 2. Download all files from shared folder
<a name="demo3"></a>
![](images/downloadFolder_sample.png)

When above structure is downloaded, the command is like below. At that time, the folder ID is the folder ID of "sampleFolder1".

![](images/demo3.gif)

Files are downloaded from the shared folder. In this demonstration, the fake folder ID and API key are used.

<a name="retrieveapikey"></a>
### Retrieve API key
In order to use this, please retrieve API key as the following flow.

1. Login to Google.
2. Access to [https://console.cloud.google.com/?hl=en](https://console.cloud.google.com/?hl=en).
3. Click select project at the right side of "Google Cloud Platform" of upper left of window.
4. Click "NEW PROJECT"
    1. Input "Project Name".
    2. Click "CREATE".
    3. Open the created project.
    4. Click "Enable APIs and get credentials like keys".
    5. Click "Library" at left side.
    6. Input "Drive API" in "Search for APIs & Services".
    7. Click "Google Drive API".
    8. Click "ENABLE".
    9. Back to [https://console.cloud.google.com/?hl=en](https://console.cloud.google.com/?hl=en).
    10. Click "Enable APIs and get credentials like keys".
    11. Click "Credentials" at left side.
    12. Click "Create credentials" and select API key.
    13. Copy the API key. You can use this API key.

### Download
When the URL of shared folder is ``https://drive.google.com/drive/folders/#####?usp=sharing``, you can download all files in the folder by the following command.

~~~bash
$ goodls -u https://drive.google.com/drive/folders/#####?usp=sharing -key [APIkey]
~~~

- Project files cannot be downloaded by API key. If you want to download the project files, you can download them by [ggsrun](), because ggsrun uses OAuth2.
- This new function uses the Go library of [go-getfilelist](https://github.com/tanaikech/go-getfilelist).
- When the option of ``--NoProgres``, ``-np`` is used, the progress information is not seen. This is a silent mode.
- If the files which are tried to be downloaded are existing, an error occurs. But when you use the option ``--overwrite`` and ``--skip``, the files are overwritten and skipped, respectively.

### Retrieve information of file and folder
When you want to retrieve the information of file and folder, you can do it as follows.

#### For file
~~~bash
$ goodls -u https://docs.google.com/spreadsheets/d/#####/edit?usp=sharing -key [APIkey] -i
~~~

#### For folder
~~~bash
$ goodls -u https://drive.google.com/drive/folders/#####?usp=sharing -key [APIkey] -i
~~~

<a name="resumabledownloadoffile"></a>
## 3. Resumable download of shared file
When you use this option, at first, please retrieve API key. About how to retrieve API key, you can see at [here](#retrieveapikey).

When you want to download 100 MBytes of the shared file, you can use the following command.

~~~bash
$ goodls -u [URL of shared file on Google Drive] -key [APIkey] -r 100m
~~~

- Please use the option ``-r``. In this sample, ``100m`` means to download 100 MBytes of the shared file.
    - If you want to download 1 GB, please use ``-r 1g``.
    - If you use ``-r 1000000``, 1 MByte of the file will be able to be downloaded.

You can see the actual running of this option at the following demonstration movie.

<a name="demo4"></a>
![](images/demo4.gif)

In this demonstration, the following command is run 3 times.

~~~bash
$ goodls -u https://drive.google.com/drive/folders/abcdefg?usp=sharing -key htjklmn -r 80m
~~~

- At 1st run, the data of 0 - 80 Mbytes is downloaded.
    - You can see ``New download`` at "Current status".
- At 2nd run, the data of 80 - last is downloaded.
    - You can see ``Resumable download`` at "Current status".
- At 3rd run, the download has already been done. So the checksum is shown.
    - You can see ``Download has already done.`` at "Current status".

#### Note
- Reason that API key is used for this.
    - When it accesses to the shared file without the authorization, the file size and md5checksum cannot be retrieved. So in order to use Drive API, I adopted to use API key.
- Reason that the download size is inputted every time.
    - When this option is run 1 time, 1 quota is used for Drive API. So I adopted this way.

# Q&A
- I want to download **shared projects** from user's Google Drive.
    - You can download **shared projects** using [ggsrun](https://github.com/tanaikech/ggsrun).
    - ggsrun can also download **shared files** from other user's Google Drive using Drive API which needs the access token.
- I want to download all files including the standalone projects from the shared folder and own folder.
    - You can achieve it using [ggsrun](https://github.com/tanaikech/ggsrun).

-----

<a name="Licence"></a>
# Licence
[MIT](LICENCE)

<a name="Author"></a>
# Author
[Tanaike](https://tanaikech.github.io/about/)

If you have any questions and commissions for me, feel free to tell me.

<a name="Update_History"></a>
# Update History
* v1.0.0 (January 10, 2018)

    1. Initial release.

* v1.0.1 (January 11, 2018)

    1. In order to download several files, a datafile including URLs using Standard Input and Pipe have gotten to be able to be inputted.

* v1.0.2 (May 10, 2018)

    1. Files with large size has gotten to be able to be used.
        - In order to download files with large size (several gigabytes), files are saved by chunks.

* v1.0.3 (September 4, 2018)

    1. When the files are downloaded, the progress of downloading got to be able to be displayed.
        - This demonstration can be seen at [Demo](#demo2).
        - If the new option of ``--np`` is used, the progress is not displayed.

* v1.1.0 (November 4, 2018)

    1. By using API key, [files from **the shared folder** got to be able to be downloaded while keeping the folder structure](downloadfilesfromfolder).
        - This demonstration can be seen at [Demo](#demo3).
    1. By using API key, the information of shared file and folder can be also retrieved.
    1. About the option of ``--extension`` and ``-e``, when ``-e ms`` is used, Google Docs (Document, Spreadsheet, Slides) are converted to Microsoft Docs (Word, Excel, Powerpoint), respectively.

* v1.1.1 (November 13, 2018)

	1. Version of [go-getfilelist](https://github.com/tanaikech/go-getfilelist) was updated. Because the structure of ``drive.File`` got to be able to be used, I also updated this application.

* v1.2.0 (November 24, 2018)

    1. By using API key, the shared large files can be run [**the resumable download**](#resumabledownloadoffile).
        - This demonstration can be seen at [Demo](#demo4).


[TOP](#TOP)
