package discord

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/alex0esc/ceres/internal/task"
	"github.com/alex0esc/ceres/internal/tools"
	"github.com/bwmarrin/discordgo"
)

// downloadImageAttachments filters a message's attachments down to images,
// downloads each one, and returns them base64-encoded. Attachments that
// aren't images or that fail to download are skipped (logged, not fatal).
func (d *Discord) downloadImageAttachments(attachments []*discordgo.MessageAttachment) []task.ImageInput {
	if len(attachments) == 0 {
		return nil
	}

	images := make([]task.ImageInput, 0, len(attachments))
	for _, att := range attachments {
		if att == nil || att.URL == "" {
			continue
		}

		data, err := downloadAttachment(att.URL)
		if err != nil {
			log.Printf("discord: failed to download attachment %q: %v", att.Filename, err)
			continue
		}

		mimeType := tools.DetectImageMimeType(att.Filename, data)
		if mimeType == "" {
			// not an image, skip
			continue
		}

		images = append(images, task.ImageInput{
			Base64Image: base64.StdEncoding.EncodeToString(data),
			MimeType:    mimeType,
		})
	}

	if len(images) == 0 {
		return nil
	}
	return images
}

// downloadAttachment fetches the raw bytes of a Discord attachment URL.
func downloadAttachment(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("http get failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}
	return data, nil
}
