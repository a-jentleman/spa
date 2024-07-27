package spa

import "mime"

func init() {
	mime.AddExtensionType(".html", "text/html")
	mime.AddExtensionType(".js", "text/javascript")
	mime.AddExtensionType(".mjs", "text/javascript")
	mime.AddExtensionType(".txt", "text/plain")

	mime.AddExtensionType(".xhtml", "application/xhtml+xml")
	mime.AddExtensionType(".xml", "application/xml")
	mime.AddExtensionType(".json", "application/json")
	mime.AddExtensionType(".zip", "application/zip")

	mime.AddExtensionType(".mp3", "audio/mpeg")

	mime.AddExtensionType(".mp4", "video/mp4")
	mime.AddExtensionType(".mpeg", "video/mpeg")

	mime.AddExtensionType(".png", "image/png")
	mime.AddExtensionType(".jpg", "image/jpeg")
	mime.AddExtensionType(".jpeg", "image/jpeg")
	mime.AddExtensionType(".tif", "image/tiff")
	mime.AddExtensionType(".tiff", "image/tiff")

	mime.AddExtensionType(".ttf", "font/ttf")
}

func addMimeMapping(ext string, contentType string) {
}
