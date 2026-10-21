package main

import (
	"context"
	"errors"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"whatsapp-client/internal/wa"
)

// Group methods of waClient (api.Groups). Each change refreshes or drops the cached info.

func (w waClient) CreateGroup(ctx context.Context, name, description string, people []types.JID, parent types.JID, community bool) (wa.Group, error) {
	var info *types.GroupInfo
	var err error
	if community {
		info, err = w.createCommunity(ctx, name, description)
	} else {
		req := whatsmeow.ReqCreateGroup{Name: name, Participants: people}
		req.LinkedParentJID = parent
		info, err = w.c.CreateGroup(ctx, req)
	}
	if err != nil {
		return wa.Group{}, err
	}
	w.groups.Put(info)
	return wa.GroupFrom(info), nil
}

func (w waClient) GroupInfo(ctx context.Context, jid types.JID) (wa.Group, error) {
	info, err := w.c.GetGroupInfo(ctx, jid)
	if err != nil {
		return wa.Group{}, err
	}
	w.groups.Put(info)
	return wa.GroupFrom(info), nil
}

func (w waClient) UpdateParticipants(ctx context.Context, jid types.JID, action string, people []types.JID) ([]wa.Participant, error) {
	change := whatsmeow.ParticipantChangeAdd
	if action == "promote" {
		change = whatsmeow.ParticipantChangePromote
	}
	res, err := w.c.UpdateGroupParticipants(ctx, jid, people, change)
	w.groups.Forget(jid)
	if err != nil {
		return nil, err
	}
	return wa.Participants(res), nil
}

func (w waClient) InviteLink(ctx context.Context, jid types.JID, reset bool) (string, error) {
	return w.c.GetGroupInviteLink(ctx, jid, reset)
}

func (w waClient) LinkGroup(ctx context.Context, parent, child types.JID) error {
	w.groups.Forget(child)
	return w.c.LinkGroup(ctx, parent, child)
}

func (w waClient) UnlinkGroup(ctx context.Context, child types.JID) error {
	info, err := w.c.GetGroupInfo(ctx, child)
	if err != nil {
		return err
	}
	if info.LinkedParentJID.IsEmpty() {
		return errors.New("group is not in a community")
	}
	w.groups.Forget(child)
	return w.c.UnlinkGroup(ctx, info.LinkedParentJID, child)
}

func (w waClient) LeaveGroup(ctx context.Context, jid types.JID) error {
	w.groups.Forget(jid)
	return w.c.LeaveGroup(ctx, jid)
}

// createCommunity sends wa.CommunityCreateNode instead of whatsmeow's CreateGroup(IsParent),
// which WhatsApp rejects with 400 bad-request. Participants join through the topic groups.
func (w waClient) createCommunity(ctx context.Context, name, description string) (*types.GroupInfo, error) {
	internals := w.c.DangerousInternals()
	resp, err := internals.SendGroupIQ(ctx, "set", types.GroupServerJID,
		wa.CommunityCreateNode(name, description, w.c.GenerateMessageID()[:12]))
	if err != nil {
		return nil, err
	}
	groupNode, ok := resp.GetOptionalChildByTag("group")
	if !ok {
		return nil, errors.New("no <group> in the response to create community")
	}
	return internals.ParseGroupNode(&groupNode)
}
