package app

import (
	"bufio"
	"os"
	internal "ownbox-cli/app/src"
	"path/filepath"
	"strings"
)

func Download(filename, hash string) error {

	err := internal.DownloadFile(filename, hash)
	if err != nil {
		return err
	}

	return nil
}

func Upload(filepath string) error {

	err := internal.UploadFile(filepath)
	if err != nil {
		return err
	}

	return nil
}

func CreateAccount() error {
	reader := bufio.NewReader(os.Stdin)
	passwordReader := bufio.NewReader(os.Stdin)

	log, err := reader.ReadString('\n')
	if err != nil {
		return err
	}

	psw, err := passwordReader.ReadString('\n')

	str := strings.TrimSpace(log)
	psw = strings.TrimSpace(psw)

	login := str
	password := string(psw)

	err = internal.SaveInCfg(login, password)
	if err != nil {
		// errors handling
	}

	err = internal.SendLoginToOwnBox(login, password)
	if err != nil {
		return err
	}

	return nil
}

func DeleteAccount() error {

	err := internal.DeleteAccount()
	if err != nil {
		return err
	}

	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}

	cfgPath := filepath.Join(cfgDir, "ownbox")
	f := filepath.Join(cfgPath, "conf.json")

	if err := os.Remove(f); err != nil {
		return err
	}

	return nil
}

func DeleteItem(hash string) error {

	err := internal.DeleteItemFromStorage(hash)
	if err != nil {
		return err
	}

	return nil
}
