package fileutil

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"ncloud-api/models"
	"os"
	"path/filepath"
	"syscall"

	"github.com/google/uuid"
)

func CopyDirectory(scrDir, dest string) error {
	entries, err := os.ReadDir(scrDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(scrDir, entry.Name())
		destPath := filepath.Join(dest, entry.Name())

		fmt.Println(sourcePath, destPath)

		fileInfo, err := os.Stat(sourcePath)
		if err != nil {
			return err
		}

		stat, ok := fileInfo.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("failed to get raw syscall.Stat_t data for '%s'", sourcePath)
		}

		switch fileInfo.Mode() & os.ModeType {
		case os.ModeDir:
			if err := CreateIfNotExists(destPath, 0755); err != nil {
				return err
			}
			if err := CopyDirectory(sourcePath, destPath); err != nil {
				return err
			}
		case os.ModeSymlink:
			if err := CopySymLink(sourcePath, destPath); err != nil {
				return err
			}
		default:
			if err := Copy(sourcePath, destPath); err != nil {
				return err
			}
		}

		if err := os.Lchown(destPath, int(stat.Uid), int(stat.Gid)); err != nil {
			return err
		}

		fInfo, err := entry.Info()
		if err != nil {
			return err
		}

		isSymlink := fInfo.Mode()&os.ModeSymlink != 0
		if !isSymlink {
			if err := os.Chmod(destPath, fInfo.Mode()); err != nil {
				return err
			}
		}
	}
	return nil
}

func Copy(srcFile, dstFile string) error {
	out, err := os.Create(dstFile)
	if err != nil {
		return err
	}

	defer out.Close()

	in, err := os.Open(srcFile)
	defer in.Close()
	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	return nil
}

func Exists(filePath string) bool {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return false
	}

	return true
}

func CreateIfNotExists(dir string, perm os.FileMode) error {
	if Exists(dir) {
		return nil
	}

	if err := os.MkdirAll(dir, perm); err != nil {
		return fmt.Errorf("failed to create directory: '%s', error: '%s'", dir, err.Error())
	}

	return nil
}

func CopySymLink(source, dest string) error {
	link, err := os.Readlink(source)
	if err != nil {
		return err
	}
	return os.Symlink(link, dest)
}

func CreateZip(zipWriter *zip.Writer, directory string, fileList [][]string) error {
	for _, file := range fileList {
		fileName := file[1]
		fileId := file[0]
		if _, err := uuid.Parse(fileId); err != nil {
			return errors.New("invalid filename")
		}
		filePath := "/var/ncloud_upload/" + directory + "/" + fileId

		err := CopyFileToZipWriter(zipWriter, filePath, fileName)
		if err != nil {
			return fmt.Errorf("error copying content for %s: %w", filePath, err)
		}
	}

	return nil
}

func CopyFileToZipWriter(zipWriter *zip.Writer, filePath string, fileName string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("error opening file %s: %w", filePath, err)
	}
	defer file.Close()

	if err != nil {
		return fmt.Errorf("error getting file info for %s: %w", filePath, err)
	}

	isValidFileName := models.IsValidFileName(fileName)
	if !isValidFileName {
		return fmt.Errorf("invalid file name: %s", fileName)
	}

	header := &zip.FileHeader{
		Name:   fileName,
		Method: zip.Deflate,
	}

	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("error creating header for %s: %w", filePath, err)
	}
	_, err = io.Copy(writer, file)

	return nil
}
