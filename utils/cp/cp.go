package cp

import (
	"fmt"
	"io"
	"os"
)

func TouchFile(filePath string) error {
	file, err := os.OpenFile(filePath, os.O_RDONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	return file.Close()
}

func cp(src, dst string) error {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()
	_, err = io.Copy(destination, source)
	return err
}

func CopyFile(src, dst string) (err error) {
	dstTmp := fmt.Sprintf("%s.tmp", dst)
	if err := cp(src, dstTmp); err != nil {
		return fmt.Errorf("failed to copy file: %s", err)
	}

	err = os.Rename(dstTmp, dst)
	if err != nil {
		return fmt.Errorf("failed to rename file: %s", err)
	}

	si, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat file: %s", err)
	}
	err = os.Chmod(dst, si.Mode())
	if err != nil {
		return fmt.Errorf("failed to chmod file: %s", err)
	}

	return nil
}

func InstallBinaries(pluginBins []string, hostCNIBinPath string) error {
	for _, plugin := range pluginBins {
		target := fmt.Sprintf("%s/%s", hostCNIBinPath, plugin)
		source := fmt.Sprintf("%s", plugin)

		if err := CopyFile(source, target); err != nil {
			return fmt.Errorf("Failed to install %s: %s", target, err)
		}
		fmt.Printf("Installed %s\n", target)
	}
	return nil
}

func InstallBinariesFromDir(readDir string, hostCNIBinPath string, excludeBins map[string]bool) error {
	bins, err := os.ReadDir(readDir)
	if err != nil {
		return fmt.Errorf("failed to read directory %s, error: %s", readDir, err)
	}

	for _, file := range bins {
		// Only copy files
		if !file.Type().IsRegular() {
			continue
		}
		// Exclude binaries in deny-list
		if _, ok := excludeBins[file.Name()]; ok {
			continue
		}
		target := fmt.Sprintf("%s/%s", hostCNIBinPath, file.Name())
		source := fmt.Sprintf("%s", file.Name())
		if err := CopyFile(source, target); err != nil {
			return fmt.Errorf("Failed to install %s: %s", target, err)
		}
	}
	return nil
}
