// photos.go
package tg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/gotd/td/telegram/downloader"
	tgapi "github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/diamondburned/gotk4/examples/telegram/internal/model"
)

const maxPhotoBytes = 10 << 20 // 10 MB

func pickSize(sizes []tgapi.PhotoSizeClass) (string, bool) {
	best, smallest := "", ""
	bestW, smallestW := 0, int(^uint(0)>>1)
	for _, sc := range sizes {
		var typ string
		var w int
		switch s := sc.(type) {
		case *tgapi.PhotoSize:
			typ, w = s.Type, s.W
		case *tgapi.PhotoSizeProgressive:
			typ, w = s.Type, s.W
		default:
			continue // stripped/path/cached: no real dimensions
		}
		if w <= 800 && w > bestW {
			best, bestW = typ, w
		}
		if w < smallestW {
			smallest, smallestW = typ, w
		}
	}
	if best != "" {
		return best, true
	}
	if smallest != "" {
		return smallest, true
	}
	return "", false
}

var sizeTypeRe = regexp.MustCompile(`^[a-z]$`)

func cachePath(root string, photoID int64, sizeType string) (string, error) {
	if !sizeTypeRe.MatchString(sizeType) {
		return "", fmt.Errorf("invalid size type %q", sizeType)
	}
	name := fmt.Sprintf("%x_%s.jpg", photoID, sizeType)
	p := filepath.Join(root, name)
	if filepath.Dir(p) != filepath.Clean(root) {
		return "", fmt.Errorf("cache path escapes root")
	}
	return p, nil
}

// DownloadPhoto downloads ref into cacheDir. Cached files reused. One retry
// on FILE_REFERENCE_EXPIRED via a peer-kind-aware message refetch.
func (c *Client) DownloadPhoto(ctx context.Context, ref model.PhotoRef, cacheDir string) (string, error) {
	api := c.api()
	if api == nil {
		return "", errors.New("not connected")
	}
	if ref.SizeType == "" {
		return "", fmt.Errorf("photo has no renderable size")
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", err
	}
	path, err := cachePath(cacheDir, ref.PhotoID, ref.SizeType)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	dl := func(r model.PhotoRef) error {
		loc := &tgapi.InputPhotoFileLocation{
			ID: r.PhotoID, AccessHash: r.AccessHash,
			FileReference: r.FileRef, ThumbSize: r.SizeType,
		}
		_, err := downloader.NewDownloader().WithPartSize(512*1024).
			Download(api, loc).ToPath(ctx, path)
		return err
	}

	err = dl(ref)
	if err != nil && tgerr.Is(err, "FILE_REFERENCE_EXPIRED") {
		fresh, ferr := c.refreshPhotoRef(ctx, api, ref)
		if ferr != nil {
			return "", ferr
		}
		err = dl(fresh)
	}
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if fi, serr := os.Stat(path); serr == nil && fi.Size() > maxPhotoBytes {
		_ = os.Remove(path)
		return "", fmt.Errorf("photo exceeds %d bytes", maxPhotoBytes)
	}
	return path, nil
}

// refreshPhotoRef refetches the carrying message for a fresh file_reference.
// Channel/supergroup messages need ChannelsGetMessages (messages.getMessages
// does NOT return them — verified against gotd v0.159.0 / Telegram API).
func (c *Client) refreshPhotoRef(ctx context.Context, api *tgapi.Client, ref model.PhotoRef) (model.PhotoRef, error) {
	if api == nil {
		return ref, errors.New("not connected")
	}
	var msgs []tgapi.MessageClass
	if ref.Kind == model.PeerChannel {
		ch, ok := c.peers.InputChannel(ref.PeerID)
		if !ok {
			return ref, errUnknownPeer
		}
		res, err := api.ChannelsGetMessages(ctx, &tgapi.ChannelsGetMessagesRequest{
			Channel: ch,
			ID:      []tgapi.InputMessageClass{&tgapi.InputMessageID{ID: ref.MsgID}},
		})
		if err != nil {
			return ref, err
		}
		if v, ok := res.(*tgapi.MessagesChannelMessages); ok {
			msgs = v.Messages
		}
	} else {
		res, err := api.MessagesGetMessages(ctx, []tgapi.InputMessageClass{
			&tgapi.InputMessageID{ID: ref.MsgID},
		})
		if err != nil {
			return ref, err
		}
		if v, ok := res.(*tgapi.MessagesMessages); ok {
			msgs = v.Messages
		}
	}
	for _, mc := range msgs {
		m, ok := mc.(*tgapi.Message)
		if !ok || m.ID != ref.MsgID {
			continue
		}
		if mp, ok := m.Media.(*tgapi.MessageMediaPhoto); ok {
			if p, ok := mp.Photo.(*tgapi.Photo); ok {
				ref.FileRef = p.FileReference
				return ref, nil
			}
		}
	}
	return ref, fmt.Errorf("photo not found in refetched message")
}
