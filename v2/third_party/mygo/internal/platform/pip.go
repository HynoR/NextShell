package platform

// TogglePictureInPicture is the script that moves the page's video into
// picture in picture, or the one out back, for backends whose web view
// has no call for it: the video playing, else the largest that loaded, of
// the page's document. It runs as a user's gesture, which
// requestPictureInPicture() needs.
const TogglePictureInPicture = `(() => {
	if (document.pictureInPictureElement) {
		document.exitPictureInPicture().catch(() => {});
		return;
	}
	let video, most = -1;
	for (const v of document.querySelectorAll("video")) {
		if (v.disablePictureInPicture || v.readyState === 0) continue;
		const r = v.getBoundingClientRect(), score = (v.paused ? 0 : 1e9) + r.width * r.height;
		if (score > most) { video = v; most = score; }
	}
	if (video) video.requestPictureInPicture().catch(() => {});
})()`

// PictureInPictureActive is the body of an async function that tells
// whether a video of the page's document is in picture in picture: "true"
// or "false".
const PictureInPictureActive = "return String(!!document.pictureInPictureElement)"
