package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Activer007/SupaShare/cmd/cli-uploader/cliapi"
	"github.com/Activer007/SupaShare/cmd/cli-uploader/cliconfig"
	"github.com/Activer007/SupaShare/cmd/cli-uploader/cliconstants"
	"github.com/Activer007/SupaShare/cmd/cli-uploader/cliflags"
	"github.com/Activer007/SupaShare/internal/environment"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/safedisplay"
)

func main() {
	mode := cliflags.Parse()
	switch mode {
	case cliflags.ModeLogin:
		doLogin()
	case cliflags.ModeLogout:
		doLogout()
	case cliflags.ModeUpload:
		processUpload(cliflags.ModeUpload)
	case cliflags.ModeArchive:
		processUpload(cliflags.ModeArchive)
	case cliflags.ModeDownload:
		processDownload()
	case cliflags.ModeInvalid:
		os.Exit(3)
	}
}

func doLogin() {
	checkDockerFolders()
	cliconfig.CreateLogin()
}

func processUpload(mode int) {
	cliconfig.Load()
	uploadParam := cliflags.GetUploadParameters(mode)

	if mode == cliflags.ModeArchive {
		zipPath, err := zipFolder(uploadParam.Directory, uploadParam.TmpFolder, !uploadParam.JsonOutput)
		if err != nil {
			fmt.Println(safedisplay.Text(err.Error()))
			os.Exit(1)
		}
		uploadParam.File = zipPath
		defer deleteTempFolder(zipPath) // ensure cleanup
	}

	// Perform the upload
	result, err := cliapi.UploadFile(uploadParam)
	if err != nil {
		fmt.Println()
		if errors.Is(err, cliapi.ErrUnauthorised) {
			fmt.Println("ERROR: Unauthorised API key. Please re-run login or make sure that the API key has the permission to upload files.")
		} else {
			fmt.Println("ERROR: Could not upload file")
			fmt.Println(safedisplay.Text(err.Error()))
		}
		os.Exit(1)
	}

	writeUploadResult(os.Stdout, result, uploadParam.JsonOutput)
}

func writeUploadResult(out io.Writer, result models.FileApiOutput, jsonOutput bool) {
	if jsonOutput {
		_ = json.NewEncoder(out).Encode(result)
		return
	}
	fmt.Fprintln(out, "Upload successful")
	fmt.Fprintln(out, "File Name: "+safedisplay.Text(result.Name))
	fmt.Fprintln(out, "File ID: "+safedisplay.Text(result.Id))
	fmt.Fprintln(out, "File Download URL: "+safedisplay.Text(result.UrlDownload))
}

func processDownload() {
	cliconfig.Load()
	uploadParam := cliflags.GetUploadParameters(cliflags.ModeDownload)

	// Perform the download
	err := cliapi.DownloadFile(uploadParam)
	if err != nil {
		fmt.Println()
		if errors.Is(err, cliapi.ErrUnauthorised) {
			fmt.Println("ERROR: Unauthorised API key. Please re-run login or make sure that the API key has the permission to download files.")
		} else {
			fmt.Println("ERROR: Could not download file")
			fmt.Println(safedisplay.Text(err.Error()))
		}
		os.Exit(1)
	}
}

func doLogout() {
	err := cliconfig.Delete()
	if err != nil {
		fmt.Println("ERROR: Could not delete configuration file")
		fmt.Println(safedisplay.Text(err.Error()))
		os.Exit(2)
	}
	fmt.Println("Logged out. To login again, run: supashare-cli login")
}

func checkDockerFolders() {
	if !environment.IsDockerInstance() {
		return
	}
	_, isDefault := cliflags.GetConfigLocation()
	if !isDefault {
		return
	}
	if !helper.FolderExists(cliconstants.DockerFolderConfig) {
		fmt.Println("Warning: Docker folder does not exist, configuration will be lost when creating a new container")
		helper.CreateDir(cliconstants.DockerFolderConfig)
	}
	if !helper.FolderExists(cliconstants.DockerFolderUpload) {
		helper.CreateDir(cliconstants.DockerFolderUpload)
	}
}

func deleteTempFolder(path string) {
	folder := filepath.Dir(path)
	_ = os.RemoveAll(folder)
}
