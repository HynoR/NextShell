//go:build darwin

package darwin

import (
	"cmp"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/internal/platform"
)

var enableNotificationSenders sync.Once

// EnableNotificationSenders registers the opt-in translator before a sender
// notification can reach the backend. Later constructors only read the Once.
func EnableNotificationSenders() {
	enableNotificationSenders.Do(func() { prepareCommunication = (*Backend).addCommunicationNotification })
}

var communicationAuthorization struct {
	once    sync.Once
	granted bool
}

func communicationAuthorized() bool {
	communicationAuthorization.once.Do(func() { communicationAuthorization.granted = checkCommunicationAuthorization() })
	return communicationAuthorization.granted
}

func checkCommunicationAuthorization() bool {
	lib, err := purego.Dlopen("/System/Library/Frameworks/Intents.framework/Intents", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil || lib == 0 || !hasClass("INSendMessageIntent") || !sendBool(class("UNMutableNotificationContent"), "instancesRespondToSelector:", uintptr(sel("contentByUpdatingWithProvider:error:"))) {
		return false
	}
	security, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW)
	if err != nil {
		return false
	}
	var createTask func(uintptr) uintptr
	var copyEntitlement func(uintptr, id, uintptr) id
	purego.RegisterLibFunc(&createTask, security, "SecTaskCreateFromSelf")
	purego.RegisterLibFunc(&copyEntitlement, security, "SecTaskCopyValueForEntitlement")
	task := createTask(0)
	if task == 0 {
		return false
	}
	defer cfRelease(task)
	value := copyEntitlement(task, nsString("com.apple.developer.usernotifications.communication"), 0)
	if value == 0 {
		return false
	}
	defer cfRelease(uintptr(value))
	return sendBool(value, "boolValue")
}

// communicationIntent returns an owned intent, or nil for an incomplete sender.
func communicationIntent(n *platform.Notification) id {
	s := n.Sender
	if s == nil || s.ID == "" || len(s.Avatar) == 0 {
		return 0
	}
	data := communicationAvatar(s.Avatar)
	if data == 0 {
		return 0
	}
	// IDs are app-defined, so they are not inferred to be contact details.
	handle := send(send(class("INPersonHandle"), "alloc"), "initWithValue:type:", uintptr(nsString(s.ID)), 0)
	defer send(handle, "release")
	image := send(class("INImage"), "imageWithImageData:", uintptr(data))
	person := send(send(class("INPerson"), "alloc"), "initWithPersonHandle:nameComponents:displayName:image:contactIdentifier:customIdentifier:isMe:suggestionType:",
		uintptr(handle), 0, uintptr(nsString(s.Name)), uintptr(image), 0, uintptr(nsString(s.ID)), 0, 0)
	defer send(person, "release")
	body := n.Body
	if n.Subtitle != "" {
		body = n.Subtitle
		if n.Body != "" {
			body += "\n" + n.Body
		}
	}
	return send(send(class("INSendMessageIntent"), "alloc"), "initWithRecipients:outgoingMessageType:content:speakableGroupName:conversationIdentifier:serviceName:sender:attachments:",
		0, 1, uintptr(nsString(body)), 0, uintptr(nsString(cmp.Or(n.Group, n.ID))), 0, uintptr(person), 0)
}

func communicationInteraction(intent id) id {
	interaction := send(send(class("INInteraction"), "alloc"), "initWithIntent:response:", uintptr(intent), 0)
	send(interaction, "setDirection:", 2) // incoming
	return interaction
}

// communicationContent keeps the ordinary content when conversion fails.
func communicationContent(content, intent id) id {
	prepared := autorelease(send(content, "mutableCopy"))
	send(prepared, "setBody:", uintptr(send(intent, "content")))
	var nativeErr id
	updated := send(prepared, "contentByUpdatingWithProvider:error:", uintptr(intent), uintptr(unsafe.Pointer(&nativeErr)))
	if nativeErr != 0 || updated == 0 {
		return content
	}
	return updated
}

type preparingNotification struct{ done func(error) }

func (p *preparingNotification) finish(err error) {
	if done := p.done; done != nil {
		p.done = nil
		done(err)
	}
}

func (b *Backend) addCommunicationNotification(n *platform.Notification, content id, done func(error)) bool {
	if !communicationAuthorized() {
		return false
	}
	intent := communicationIntent(n)
	if intent == 0 {
		return false
	}
	if b.notifyPreparing == nil {
		b.notifyPreparing = make(map[string]func())
	}
	p := &preparingNotification{done: done}
	b.notifyPreparing[n.ID] = func() { p.finish(nil) }
	send(content, "retain")
	interaction := communicationInteraction(intent)
	blk := newBlock(func(_ objc.Block, nativeErr id) {
		failed := nativeErr != 0
		b.runOnMain(func() {
			withPool(func() {
				defer send(content, "release")
				defer send(intent, "release")
				defer send(interaction, "release")
				// Close, Clear or replacement may have canceled this while donating.
				if p.done == nil {
					return
				}
				delete(b.notifyPreparing, n.ID)
				ready := content
				if !failed {
					ready = communicationContent(content, intent)
				}
				b.submitNotification(n, ready, p.finish)
			})
		})
	})
	send(interaction, "donateInteractionWithCompletion:", uintptr(blk))
	blk.Release()
	return true
}

// communicationAvatar returns image bytes accepted by the notification image service.
func communicationAvatar(avatar []byte) id {
	data := nsData(avatar)
	decoded := send(send(class("NSImage"), "alloc"), "initWithData:", uintptr(data))
	if decoded == 0 {
		return 0
	}
	defer send(decoded, "release")
	// NSImage accepts SVG, but the Intents image service requires a raster image.
	png := pngFromImage(decoded)
	if len(png) == 0 {
		return 0
	}
	return nsData(png)
}
