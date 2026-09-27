package sqlite

import (
	"context"
	"database/sql"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

// Equal timestamps have no causal order: removals win, then event ID and emoji
// provide deterministic convergence across live delivery, history and aliases.
const reactionOrder = `at_ms DESC, (emoji='') DESC, event_id DESC, emoji DESC`
const mergeReaction = `emoji=excluded.emoji, at_ms=excluded.at_ms, event_id=excluded.event_id
 WHERE (excluded.at_ms, excluded.emoji='', excluded.event_id, excluded.emoji) >
 (message_reactions.at_ms, message_reactions.emoji='', message_reactions.event_id, message_reactions.emoji)`

func (s *Store) SaveReaction(ctx context.Context, accountID string, reaction core.Reaction) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	newLink := false
	if !reaction.IsOwn && reaction.ParticipantAlias != "" {
		aliases, err := linkedJIDsTx(ctx, tx, accountID, []string{reaction.ParticipantID})
		if err != nil {
			return nil, err
		}
		newLink = !slices.Contains(aliases, reaction.ParticipantAlias)
	}
	changed, err := saveReactionTx(ctx, tx, accountID, reaction)
	if err != nil {
		return nil, err
	}
	var ids []string
	if changed || newLink {
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT conversation_id FROM conversation_aliases WHERE account_id=? AND (jid=? OR ?)`, accountID, reaction.ChatID, newLink)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ids, nil
}

func saveReactionTx(ctx context.Context, tx *sql.Tx, accountID string, r core.Reaction) (bool, error) {
	if accountID == "" || r.ChatID == "" || r.TargetID == "" || (!r.IsOwn && r.ParticipantID == "") || r.At.IsZero() || r.EventID == "" || len(r.Emoji) > 128 {
		return false, core.ErrInvalid
	}
	participant := "self"
	if !r.IsOwn {
		if r.ParticipantAlias != "" {
			if err := addChatLinkTx(ctx, tx, accountID, core.ChatLink{First: r.ParticipantID, Second: r.ParticipantAlias}); err != nil {
				return false, err
			}
		}
		aliases, err := linkedJIDsTx(ctx, tx, accountID, []string{r.ParticipantID})
		if err != nil {
			return false, err
		}
		sort.Strings(aliases)
		participant = aliases[0]
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO message_reactions(account_id,chat_id,target_id,participant_id,emoji,at_ms,event_id)
 VALUES(?,?,?,?,?,?,?) ON CONFLICT(account_id,chat_id,target_id,participant_id) DO UPDATE SET `+mergeReaction, accountID, r.ChatID, r.TargetID, participant, r.Emoji, r.At.UnixMilli(), r.EventID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

func mergeReactionParticipantsTx(ctx context.Context, tx *sql.Tx, accountID, participant string) error {
	aliases, err := linkedJIDsTx(ctx, tx, accountID, []string{participant})
	if err != nil {
		return err
	}
	sort.Strings(aliases)
	for _, alias := range aliases[1:] {
		if _, err := tx.ExecContext(ctx, `INSERT INTO message_reactions(account_id,chat_id,target_id,participant_id,emoji,at_ms,event_id)
 SELECT account_id,chat_id,target_id,?,emoji,at_ms,event_id FROM message_reactions WHERE account_id=? AND participant_id=?
 ON CONFLICT(account_id,chat_id,target_id,participant_id) DO UPDATE SET `+mergeReaction, aliases[0], accountID, alias); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM message_reactions WHERE account_id=? AND participant_id=?`, accountID, alias); err != nil {
			return err
		}
	}
	return nil
}

// Rank before filtering removals: a tombstone on one chat alias overrides older
// reactions on another alias, including when the target arrived after the event.
const rankedReactions = `SELECT m.public_id, r.participant_id, r.emoji, r.at_ms,
 ROW_NUMBER() OVER (PARTITION BY m.public_id,r.participant_id ORDER BY ` + reactionOrder + `) AS rank
 FROM messages m JOIN conversation_aliases a ON a.account_id=m.account_id AND a.conversation_id=m.conversation_id
 JOIN message_reactions r ON r.account_id=m.account_id AND r.chat_id=a.jid AND r.target_id=m.provider_message_id `

func (s *Store) attachReactions(ctx context.Context, messages []core.Message) error {
	if len(messages) == 0 {
		return nil
	}
	marks, args := make([]string, len(messages)), make([]any, len(messages))
	indexes := map[string]int{}
	for i, m := range messages {
		marks[i] = "?"
		args[i] = m.ID
		indexes[m.ID] = i
	}
	rows, err := s.db.QueryContext(ctx, `WITH ranked AS (`+rankedReactions+` WHERE m.public_id IN (`+strings.Join(marks, ",")+`))
 SELECT public_id,emoji,count(*),max(participant_id='self') FROM ranked WHERE rank=1 AND emoji!='' GROUP BY public_id,emoji ORDER BY public_id,count(*) DESC,emoji`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var r core.ReactionSummary
		if err := rows.Scan(&id, &r.Emoji, &r.Count, &r.Own); err != nil {
			return err
		}
		messages[indexes[id]].Reactions = append(messages[indexes[id]].Reactions, r)
	}
	return rows.Err()
}

func (s *Store) ListMessageReactions(ctx context.Context, id, after string, limit int) ([]core.MessageReaction, error) {
	if limit < 1 || limit > 201 {
		return nil, core.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `WITH ranked AS (`+rankedReactions+` WHERE m.public_id=?)
 SELECT participant_id,emoji,at_ms FROM ranked WHERE rank=1 AND emoji!='' AND participant_id>? ORDER BY participant_id LIMIT ?`, id, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []core.MessageReaction{}
	for rows.Next() {
		var r core.MessageReaction
		var ms int64
		if err := rows.Scan(&r.ParticipantID, &r.Emoji, &ms); err != nil {
			return nil, err
		}
		r.IsOwn = r.ParticipantID == "self"
		r.At = time.UnixMilli(ms).UTC()
		items = append(items, r)
	}
	return items, rows.Err()
}
