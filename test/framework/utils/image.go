package utils

func GetTestImage(registry string, image string) string {
	return registry + "/" + image
}
