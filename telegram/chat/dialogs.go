// Package chat provides Telegram dialog operations.
package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"agent-telegram/telegram/types"
	"github.com/gotd/td/tg"
)

// GetChats returns the list of dialogs/chats with pagination.
func (c *Client) GetChats(ctx context.Context, params *types.GetChatsParams) (*types.GetChatsResult, error) {
	if err := c.CheckInitialized(); err != nil {
		return nil, err
	}
	if params == nil {
		params = &types.GetChatsParams{}
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}

	request := &tg.MessagesGetDialogsRequest{
		Limit:      params.Limit,
		OffsetDate: 0,
		OffsetID:   0,
		OffsetPeer: &tg.InputPeerEmpty{},
	}
	position := 0
	if params.Cursor != "" {
		cursor, err := decodeDialogCursor(params.Cursor)
		if err != nil {
			return nil, err
		}
		position = cursor.Seen
		request = cursor.request(params.Limit)
	}

	result := &types.GetChatsResult{
		Chats:  make([]map[string]any, 0, params.Limit),
		Limit:  params.Limit,
		Offset: params.Offset,
	}
	seen := 0
	currentCursor := params.Cursor
	for {
		data, err := c.getDialogPage(ctx, request)
		if err != nil {
			return nil, err
		}
		if result.Total == 0 {
			result.Total = data.total
		}

		chatMap := buildChatMap(data.chats)
		userMap := buildUserMap(data.users)
		pageChats := convertDialogsToResult(data.dialogs, chatMap, userMap)
		start := 0
		if seen+len(pageChats) <= params.Offset {
			seen += len(pageChats)
		} else {
			if params.Offset > seen {
				start = params.Offset - seen
			}
			end := min(start+params.Limit-len(result.Chats), len(pageChats))
			if start < end {
				result.Chats = append(result.Chats, pageChats[start:end]...)
			}
			seen += len(pageChats)
		}

		position += len(data.dialogs)
		nextCursor, hasMore, err := nextDialogCursor(data, request.Limit, position)
		if err != nil {
			return nil, err
		}
		result.HasMore = hasMore
		result.NextCursor = nextCursor
		if len(result.Chats) >= params.Limit || !hasMore {
			break
		}
		if nextCursor == currentCursor {
			return nil, fmt.Errorf("failed to paginate dialogs: cursor did not advance")
		}
		cursor, err := decodeDialogCursor(nextCursor)
		if err != nil {
			return nil, err
		}
		currentCursor = nextCursor
		request = cursor.request(params.Limit)
	}

	result.Count = len(result.Chats)
	return result, nil
}

func (c *Client) getDialogPage(ctx context.Context, request *tg.MessagesGetDialogsRequest) (*dialogData, error) {
	dialogsClass, err := c.API().MessagesGetDialogs(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to get dialogs: %w", err)
	}

	data, err := extractDialogData(dialogsClass)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// dialogData holds extracted dialog data.
type dialogData struct {
	dialogs  []tg.DialogClass
	chats    []tg.ChatClass
	users    []tg.UserClass
	messages []tg.MessageClass
	total    int
	complete bool
}

// extractDialogData extracts dialogs, chats, and users from the response.
func extractDialogData(dialogsClass tg.MessagesDialogsClass) (*dialogData, error) {
	switch d := dialogsClass.(type) {
	case *tg.MessagesDialogs:
		return &dialogData{
			dialogs:  d.Dialogs,
			chats:    d.Chats,
			users:    d.Users,
			messages: d.Messages,
			total:    len(d.Dialogs),
			complete: true,
		}, nil
	case *tg.MessagesDialogsSlice:
		return &dialogData{
			dialogs:  d.Dialogs,
			chats:    d.Chats,
			users:    d.Users,
			messages: d.Messages,
			total:    d.Count,
		}, nil
	case *tg.MessagesDialogsNotModified:
		return nil, fmt.Errorf("dialogs not modified")
	default:
		return nil, fmt.Errorf("unexpected dialogs type: %T", d)
	}
}

type dialogCursor struct {
	OffsetDate int    `json:"date"`
	OffsetID   int    `json:"id"`
	PeerType   string `json:"peerType"`
	PeerID     int64  `json:"peerId"`
	AccessHash int64  `json:"accessHash,omitempty"`
	Seen       int    `json:"seen"`
}

func nextDialogCursor(data *dialogData, limit, seen int) (string, bool, error) {
	if len(data.dialogs) == 0 {
		return "", false, nil
	}
	last, ok := data.dialogs[len(data.dialogs)-1].(*tg.Dialog)
	if !ok {
		return "", false, nil
	}

	cursor := dialogCursor{OffsetID: last.TopMessage, Seen: seen}
	for _, class := range data.messages {
		message, ok := class.AsNotEmpty()
		if ok && message.GetID() == last.TopMessage {
			cursor.OffsetDate = message.GetDate()
			break
		}
	}

	switch peer := last.Peer.(type) {
	case *tg.PeerUser:
		cursor.PeerType = "user"
		cursor.PeerID = peer.UserID
		for _, class := range data.users {
			if user, ok := class.(*tg.User); ok && user.ID == peer.UserID {
				cursor.AccessHash = user.AccessHash
				break
			}
		}
	case *tg.PeerChat:
		cursor.PeerType = "chat"
		cursor.PeerID = peer.ChatID
	case *tg.PeerChannel:
		cursor.PeerType = "channel"
		cursor.PeerID = peer.ChannelID
		for _, class := range data.chats {
			if channel, ok := class.(*tg.Channel); ok && channel.ID == peer.ChannelID {
				cursor.AccessHash = channel.AccessHash
				break
			}
		}
	default:
		return "", false, nil
	}

	encoded, err := encodeDialogCursor(cursor)
	if err != nil {
		return "", false, err
	}
	hasMore := !data.complete && len(data.dialogs) >= limit &&
		cursor.OffsetID > 0 && cursor.Seen < data.total
	return encoded, hasMore, nil
}

func encodeDialogCursor(cursor dialogCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode chat cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeDialogCursor(value string) (dialogCursor, error) {
	var cursor dialogCursor
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return cursor, fmt.Errorf("invalid chat cursor: %w", err)
	}
	if err := json.Unmarshal(data, &cursor); err != nil {
		return cursor, fmt.Errorf("invalid chat cursor: %w", err)
	}
	if cursor.OffsetDate < 0 || cursor.OffsetID < 0 || cursor.PeerID <= 0 || cursor.Seen < 0 {
		return cursor, fmt.Errorf("invalid chat cursor values")
	}
	switch cursor.PeerType {
	case "user", "chat", "channel":
	default:
		return cursor, fmt.Errorf("invalid chat cursor peer type")
	}
	return cursor, nil
}

func (c dialogCursor) request(limit int) *tg.MessagesGetDialogsRequest {
	request := &tg.MessagesGetDialogsRequest{
		Limit:      limit,
		OffsetDate: c.OffsetDate,
		OffsetID:   c.OffsetID,
	}
	switch c.PeerType {
	case "user":
		request.OffsetPeer = &tg.InputPeerUser{UserID: c.PeerID, AccessHash: c.AccessHash}
	case "chat":
		request.OffsetPeer = &tg.InputPeerChat{ChatID: c.PeerID}
	case "channel":
		request.OffsetPeer = &tg.InputPeerChannel{ChannelID: c.PeerID, AccessHash: c.AccessHash}
	default:
		request.OffsetPeer = &tg.InputPeerEmpty{}
	}
	return request
}

// buildChatMap builds a map of chat ID to chat class.
func buildChatMap(chats []tg.ChatClass) map[int64]tg.ChatClass {
	chatMap := make(map[int64]tg.ChatClass)
	for _, ch := range chats {
		var id int64
		switch c := ch.(type) {
		case *tg.Chat:
			id = c.ID
		case *tg.Channel:
			id = c.ID
		case *tg.ChatEmpty:
			id = c.ID
		}
		chatMap[id] = ch
	}
	return chatMap
}

// buildUserMap builds a map of user ID to user class.
func buildUserMap(users []tg.UserClass) map[int64]tg.UserClass {
	userMap := make(map[int64]tg.UserClass)
	for _, u := range users {
		if user, ok := u.(*tg.User); ok {
			userMap[user.ID] = user
		}
	}
	return userMap
}

// convertDialogsToResult converts dialogs to the result format.
func convertDialogsToResult(
	dialogs []tg.DialogClass,
	chatMap map[int64]tg.ChatClass,
	userMap map[int64]tg.UserClass,
) []map[string]any {
	result := make([]map[string]any, 0, len(dialogs))
	for _, dialogClass := range dialogs {
		dialog, ok := dialogClass.(*tg.Dialog)
		if !ok {
			continue
		}

		chatInfo := map[string]any{
			"unread_count":       dialog.UnreadCount,
			"read_inbox_max_id":  dialog.ReadInboxMaxID,
			"read_outbox_max_id": dialog.ReadOutboxMaxID,
		}

		if dialog.TopMessage > 0 {
			chatInfo["top_message_id"] = dialog.TopMessage
		}

		populateChatInfo(dialog.Peer, chatInfo, chatMap, userMap)
		result = append(result, chatInfo)
	}
	return result
}

// populateChatInfo populates chat info based on peer type.
func populateChatInfo(
	peer tg.PeerClass,
	chatInfo map[string]any,
	chatMap map[int64]tg.ChatClass,
	userMap map[int64]tg.UserClass,
) {
	switch p := peer.(type) {
	case *tg.PeerUser:
		populateUserInfo(p, chatInfo, userMap)
	case *tg.PeerChat:
		populateGroupInfo(p, chatInfo, chatMap)
	case *tg.PeerChannel:
		populateChannelInfo(p, chatInfo, chatMap)
	}
}

// populateUserInfo populates user chat information.
func populateUserInfo(p *tg.PeerUser, chatInfo map[string]any, userMap map[int64]tg.UserClass) {
	userClass, ok := userMap[p.UserID]
	if !ok {
		return
	}

	user, ok := userClass.(*tg.User)
	if !ok {
		return
	}

	chatInfo["type"] = "user"
	chatInfo["user_id"] = user.ID
	chatInfo["first_name"] = user.FirstName
	chatInfo["last_name"] = user.LastName
	chatInfo["username"] = user.Username
	if user.Bot {
		chatInfo["bot"] = true
	}

	// Add string peer for API usage
	if user.Username != "" {
		chatInfo["peer"] = "@" + user.Username
	} else {
		chatInfo["peer"] = fmt.Sprintf("user%d", user.ID)
	}
}

// populateGroupInfo populates group chat information.
func populateGroupInfo(p *tg.PeerChat, chatInfo map[string]any, chatMap map[int64]tg.ChatClass) {
	chatClass, ok := chatMap[p.ChatID]
	if !ok {
		return
	}

	chat, ok := chatClass.(*tg.Chat)
	if !ok {
		return
	}

	chatInfo["type"] = "chat"
	chatInfo["chat_id"] = chat.ID
	chatInfo["title"] = chat.Title
	chatInfo["participants_count"] = chat.ParticipantsCount

	// Add string peer for API usage (use negative chat ID as peer)
	chatInfo["peer"] = fmt.Sprintf("-%d", chat.ID)
}

// populateChannelInfo populates channel chat information.
func populateChannelInfo(p *tg.PeerChannel, chatInfo map[string]any, chatMap map[int64]tg.ChatClass) {
	chatClass, ok := chatMap[p.ChannelID]
	if !ok {
		return
	}

	channel, ok := chatClass.(*tg.Channel)
	if !ok {
		return
	}

	chatInfo["type"] = "channel"
	chatInfo["channel_id"] = channel.ID
	chatInfo["title"] = channel.Title
	chatInfo["username"] = channel.Username
	chatInfo["megagroup"] = channel.Megagroup

	// Add string peer for API usage
	if channel.Username != "" {
		chatInfo["peer"] = "@" + channel.Username
	} else {
		// Use negative channel ID as peer (channel IDs are marked with -100 prefix)
		chatInfo["peer"] = fmt.Sprintf("-100%d", channel.ID)
	}
}
