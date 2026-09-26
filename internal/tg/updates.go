package tg

import (
	"context"
	"encoding/json"
)

// AllUpdateTypes lists every update type, used when a workflow listens to
// "*" (Telegram omits some types unless requested explicitly).
var AllUpdateTypes = []string{
	"message", "edited_message", "channel_post", "edited_channel_post",
	"business_connection", "business_message", "edited_business_message",
	"deleted_business_messages", "guest_message", "message_reaction",
	"message_reaction_count", "inline_query", "chosen_inline_result",
	"callback_query", "shipping_query", "pre_checkout_query",
	"purchased_paid_media", "poll", "poll_answer", "my_chat_member",
	"chat_member", "chat_join_request", "chat_boost", "removed_chat_boost",
	"managed_bot", "subscription", "stopped_message_generation",
}

// GetUpdates long-polls for updates and returns them as generic maps so
// every field Telegram sends is available to workflows.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout int, allowed []string) ([]map[string]any, error) {
	params := map[string]any{"offset": offset, "timeout": timeout}
	if allowed != nil {
		params["allowed_updates"] = allowed
	}
	raw, err := c.call(ctx, "getUpdates", params)
	if err != nil {
		return nil, err
	}
	var ups []map[string]any
	if err := json.Unmarshal(raw, &ups); err != nil {
		return nil, err
	}
	return ups, nil
}
