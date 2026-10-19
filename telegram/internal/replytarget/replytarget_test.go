package replytarget

import (
	"testing"

	"agent-telegram/telegram/types"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

func TestBuild(t *testing.T) {
	tests := []struct {
		name               string
		in                 types.ThreadTarget
		wantReply, wantTop int
		wantNil            bool
	}{
		{name: "empty", wantNil: true},
		{name: "thread root", in: types.ThreadTarget{ThreadID: 77}, wantReply: 77},
		{name: "ordinary reply", in: types.ThreadTarget{ReplyTo: 88}, wantReply: 88},
		{name: "nested topic reply", in: types.ThreadTarget{ThreadID: 77, ReplyTo: 88}, wantReply: 88, wantTop: 77},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Build(tt.in)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("Build() = %#v, want nil", got)
				}
				return
			}
			reply, ok := got.(*tg.InputReplyToMessage)
			if !ok || reply.ReplyToMsgID != tt.wantReply || reply.TopMsgID != tt.wantTop {
				t.Fatalf("Build() = %#v, want reply=%d top=%d", got, tt.wantReply, tt.wantTop)
			}
		})
	}
}

func TestEmptyReplyEncodesPlainMessage(t *testing.T) {
	req := &tg.MessagesSendMessageRequest{Peer: &tg.InputPeerSelf{}, Message: "test", RandomID: 1, ReplyTo: Build(types.ThreadTarget{})}
	if err := req.Encode(&bin.Buffer{}); err != nil {
		t.Fatalf("plain message must encode: %v", err)
	}
}
