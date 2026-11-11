//go:build darwin

package darwin

import (
	"bytes"
	"os"
	"unsafe"
)

// attachNotificationImage is attachImage once LinkNotificationImages
// links it: the backend's methods are linked into every app, and only
// call it.
var attachNotificationImage func(content id, image []byte, done func(error)) func(error)

// LinkNotificationImages links the code of notifications' images, when
// the app first makes a notification with one.
func LinkNotificationImages() { attachNotificationImage = attachImage }

// attachImage gives a notification's content its image, as an attachment,
// which UserNotifications makes only of a file, its format told by its
// extension. The system moves the file into its own store as it adds the
// request; done, which attachImage returns wrapped, removes it if it is
// still there. An image the system does not take leaves the content
// without one.
func attachImage(content id, image []byte, done func(error)) func(error) {
	ext := ".png"
	if bytes.HasPrefix(image, []byte{0xff, 0xd8, 0xff}) {
		ext = ".jpg"
	}
	f, err := os.CreateTemp("", "mygo-notification-*"+ext)
	if err != nil {
		return done
	}
	path := f.Name()
	_, err = f.Write(image)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return done
	}
	var nsErr id
	attachment := send(class("UNNotificationAttachment"), "attachmentWithIdentifier:URL:options:error:",
		uintptr(nsString("")), uintptr(fileURL(path)), 0, uintptr(unsafe.Pointer(&nsErr)))
	if attachment == 0 {
		os.Remove(path)
		return done
	}
	send(content, "setAttachments:", uintptr(nsArray(attachment)))
	return func(err error) {
		os.Remove(path)
		done(err)
	}
}
