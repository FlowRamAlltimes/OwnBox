package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

type File struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type InternalRequest struct {
	Username string `json:"login"`
	Password string `json:"password"`
}

type ClientCfg struct {
	IP   string `json:"ip"`
	Port string `json:"port"`
}

// saves cfg in ~/.config/ownbox/conf.json
func SaveInCfg(log, pass string) error {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}

	cfgPath := filepath.Join(cfgDir, "ownbox")
	cfgFile := filepath.Join(cfgPath, "conf.json")

	if err = os.MkdirAll(cfgPath, 0755); err != nil {
		return err
	}

	tmp := File{
		Login:    log,
		Password: pass,
	}

	bytee, err := json.MarshalIndent(tmp, "", " ")

	return os.WriteFile(cfgFile, bytee, 0600)
}

func SendLoginToOwnBox(log, pass string) error {

	ip, post, err := ReadNetworkCfg()
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://%s:%s/api/v1/auth/login", ip, post)

	reqBody := InternalRequest{
		Username: log,
		Password: pass,
	}

	var dataToSend bytes.Buffer

	err = json.NewEncoder(&dataToSend).Encode(reqBody)
	if err != nil {
		return err
	}

	resp, err := http.Post(url, "application/json", &dataToSend)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("ERROR: code %d", resp.StatusCode)
	} else {
		bytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil
		}
		fmt.Printf("%s", string(bytes))
	}

	return nil
}

// helper function
func ReadCfg() (string, string, error) {
	var tmpstruct File

	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", "", err
	}

	cfgPath := filepath.Join(cfgDir, "ownbox")
	f := filepath.Join(cfgPath, "conf.json")

	bytes, err := os.ReadFile(f)
	if err != nil {
		return "", "", err
	}

	err = json.Unmarshal(bytes, &tmpstruct)
	if err != nil {
		return "", "", err
	}

	return tmpstruct.Login, tmpstruct.Password, nil
}

func UploadFile(filepth string) error {
	login, password, err := ReadCfg()
	if err != nil {
		return err
	}

	ip, post, err := ReadNetworkCfg()
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://%s:%s", ip, post)

	f, err := os.Open(filepth)
	if err != nil {
		return err
	}
	defer f.Close()

	var buf bytes.Buffer

	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", filepath.Base(filepth))
	if err != nil {
		return err
	}

	_, err = io.Copy(part, f)
	if err != nil {
		return err
	}

	err = writer.Close()
	if err != nil {
		return err
	}

	// TODO: Make file multipart!!!

	req, err := http.NewRequest("POST", url+"/api/v1/file/upload", &buf)
	if err != nil {
		return err
	}

	req.Header.Set("login", login)
	req.Header.Set("password", password)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	c := &http.Client{}

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	fmt.Printf("%s", string(bytes))

	return nil
}

func DownloadFile(filename, hash string) error {
	login, password, err := ReadCfg()
	if err != nil {
		return err
	}

	ip, post, err := ReadNetworkCfg()
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://%s:%s", ip, post)

	f, err := os.OpenFile(filename, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	defer f.Close()

	req, err := http.NewRequest("GET", url+"/api/v1/file/download/"+hash, nil)
	if err != nil {
		return err
	}

	req.Header.Set("login", login)
	req.Header.Set("password", password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

func DeleteAccount() error {
	login, password, err := ReadCfg()
	if err != nil {
		return err
	}

	ip, post, err := ReadNetworkCfg()
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://%s:%s", ip, post)

	req, err := http.NewRequest("DELETE", url+"/api/v1/auth/delete", nil)
	if err != nil {
		return err
	}

	req.Header.Set("login", login)
	req.Header.Set("password", password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	msg, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	message := string(msg)
	fmt.Printf("%s\n", message)

	return nil
}

func DeleteItemFromStorage(hash string) error {
	login, password, err := ReadCfg()
	if err != nil {
		return err
	}

	ip, post, err := ReadNetworkCfg()
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://%s:%s", ip, post)

	req, err := http.NewRequest("DELETE", url+"/api/v1/file/delete/"+hash, nil)
	if err != nil {
		return err
	}

	req.Header.Set("login", login)
	req.Header.Set("password", password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	msg, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	message := string(msg)
	fmt.Printf("%s\n", message)

	return nil
}

func ReadNetworkCfg() (string, string, error) {
	var Cfg ClientCfg

	f, err := os.ReadFile("server.json")
	if err != nil {
		return "", "", err
	}

	err = json.Unmarshal(f, &Cfg)

	return Cfg.IP, Cfg.Port, nil
}
