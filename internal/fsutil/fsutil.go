package fsutil

import (
	"io"
	"mime/multipart"
	"os"
)

func CreateDirectory(path, name string) error {
	err := os.Mkdir(path+name, 0755)

	return err
}

func RemoveDirectory(path string) error {
	err := os.RemoveAll(path)

	return err

}

func Move(oldpath, newpath string) error {
	err := os.Rename(oldpath, newpath)

	return err
}

func SaveFile(fileHeader *multipart.FileHeader, dst string) error {
	src, err := fileHeader.Open()
	if err != nil {
		return err
	}

	defer src.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	defer out.Close()

	_, err = io.Copy(out, src)

	return err
}

func RemoveFile(path string) error {
	err := os.Remove(path)

	return err
}

func ListDirectory(path string) ([]os.DirEntry, error) {
	entry, err := os.ReadDir(path)

	return entry, err
}

func ReadFile(path string) ([]byte, error) {
	content, err := os.ReadFile(path)

	return content, err
}
