package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	tgclient "github.com/bogachenko/telegram-mcp-server/internal/telegram"
)

type telegramNotifier struct {
	botToken      string
	chatID        string
	publicBaseURL string
	client        *http.Client
}

func newTelegramNotifier(botToken string, chatID string, publicBaseURL string) *telegramNotifier {
	botToken = strings.TrimSpace(botToken)
	chatID = strings.TrimSpace(chatID)
	if botToken == "" || chatID == "" {
		return nil
	}

	return &telegramNotifier{
		botToken:      botToken,
		chatID:        chatID,
		publicBaseURL: strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (n *telegramNotifier) SendWatcherSummary(ctx context.Context, results []tgclient.SourceSyncResult) error {
	if n == nil {
		return nil
	}

	totalSaved := 0
	totalComments := 0
	perSource := make([]watcherNotificationSource, 0)

	for _, result := range results {
		if result.SavedMessages == 0 && result.SavedComments == 0 {
			continue
		}

		totalSaved += result.SavedMessages
		totalComments += result.SavedComments
		perSource = append(perSource, watcherNotificationSource{
			SourceID: result.Source.ID,
			Title:    result.Source.Title,
			Saved:    result.SavedMessages,
			Comments: result.SavedComments,
		})
	}

	if totalSaved == 0 && totalComments == 0 {
		return nil
	}

	sort.SliceStable(perSource, func(i, j int) bool {
		left := perSource[i].Saved + perSource[i].Comments
		right := perSource[j].Saved + perSource[j].Comments
		if left == right {
			return perSource[i].SourceID < perSource[j].SourceID
		}
		return left > right
	})

	return n.sendMessage(ctx, n.watcherSummaryText(totalSaved, totalComments, perSource))
}

type watcherNotificationSource struct {
	SourceID string
	Title    string
	Saved    int
	Comments int
}

func (n *telegramNotifier) watcherSummaryText(totalSaved int, totalComments int, sources []watcherNotificationSource) string {
	var builder strings.Builder

	if totalComments > 0 {
		_, _ = fmt.Fprintf(&builder, "Telegram MCP: +%d новых сообщений, +%d комментариев\n", totalSaved, totalComments)
	} else {
		_, _ = fmt.Fprintf(&builder, "Telegram MCP: +%d новых сообщений\n", totalSaved)
	}

	limit := len(sources)
	if limit > 10 {
		limit = 10
	}
	for i := 0; i < limit; i++ {
		source := sources[i]
		name := strings.TrimSpace(source.Title)
		if name == "" {
			name = source.SourceID
		}
		_, _ = fmt.Fprintf(&builder, "%s: +%d\n", name, source.Saved+source.Comments)
	}
	if len(sources) > limit {
		_, _ = fmt.Fprintf(&builder, "и ещё %d чатов\n", len(sources)-limit)
	}
	if n.publicBaseURL != "" {
		_, _ = fmt.Fprintf(&builder, "%s/admin", n.publicBaseURL)
	}

	return strings.TrimSpace(builder.String())
}

func (n *telegramNotifier) sendMessage(ctx context.Context, text string) error {
	payload := map[string]string{"chat_id": n.chatID, "text": text}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode telegram notification: %w", err)
	}

	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.botToken)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create telegram notification request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := n.client.Do(request)
	if err != nil {
		return fmt.Errorf("send telegram notification: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("send telegram notification: status %s", response.Status)
	}
	return nil
}
