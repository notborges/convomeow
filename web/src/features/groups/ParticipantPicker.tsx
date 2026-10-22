import { Cancel01Icon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { keys } from "../../api/queries";
import type { Account } from "../../api/types";
import { LiveAvatar } from "../../app/LiveAvatar";
import { Button, IconButton } from "../../ui/Button";
import { SearchField, TextField } from "../../ui/Field";
import { Loading } from "../../ui/Loading";

export type Selection = Map<string, string>;

export function ParticipantPicker({
  account,
  selected,
  exclude,
  onChange,
}: {
  account: Account;
  selected: Selection;
  exclude?: Set<string>;
  onChange: (selected: Selection) => void;
}) {
  const { t } = useTranslation();
  const [search, setSearch] = useState("");
  const [phone, setPhone] = useState("");
  const contacts = useInfiniteQuery({
    queryKey: keys.contacts(account.id, search),
    queryFn: ({ pageParam }) => api.contacts(account.id, search, pageParam),
    initialPageParam: "",
    getNextPageParam: (page) => page.next_cursor,
    enabled: !!account.capabilities?.includes("read_contacts"),
    staleTime: 15000,
  });
  const items = (
    contacts.data?.pages.flatMap((page) => page.items) ?? []
  ).filter((contact) => !exclude?.has(contact.provider_id));

  function toggle(id: string, label: string) {
    const next = new Map(selected);
    if (next.has(id)) next.delete(id);
    else next.set(id, label);
    onChange(next);
  }

  function addPhone(event: FormEvent) {
    event.preventDefault();
    const digits = phone.replace(/[^\d]/g, "");
    if (digits.length < 7 || digits.length > 15) return;
    const next = new Map(selected);
    next.set(`+${digits}`, `+${digits}`);
    onChange(next);
    setPhone("");
  }

  return (
    <div className="participant-picker">
      {selected.size > 0 && (
        <ul
          className="participant-picker__selected"
          aria-label={t(($) => $.groups.selected, { count: selected.size })}
        >
          {[...selected].map(([id, label]) => (
            <li key={id}>
              <span>{label}</span>
              <IconButton
                label={t(($) => $.groups.unselect, { name: label })}
                onClick={() => toggle(id, label)}
              >
                <HugeiconsIcon icon={Cancel01Icon} size={14} />
              </IconButton>
            </li>
          ))}
        </ul>
      )}
      <form className="participant-picker__phone" onSubmit={addPhone}>
        <TextField
          label={t(($) => $.chats.phone)}
          hint={t(($) => $.chats.countryCode)}
          type="tel"
          value={phone}
          placeholder={t(($) => $.chats.phoneExample)}
          onChange={(event) => setPhone(event.target.value)}
        />
        <Button type="submit" disabled={!phone.trim()}>
          {t(($) => $.groups.addNumber)}
        </Button>
      </form>
      <SearchField
        label={t(($) => $.chats.searchContacts)}
        value={search}
        onChange={setSearch}
      />
      <div className="participant-picker__list">
        {contacts.isPending ? (
          <Loading label={t(($) => $.chats.loadingContacts)} />
        ) : contacts.isError ? (
          <p className="form-error" role="alert">
            {t(($) => $.chats.contactsError)}
          </p>
        ) : items.length === 0 ? (
          <p className="participant-picker__empty">
            {t(($) => $.chats.noContacts)}
          </p>
        ) : (
          items.map((contact) => (
            <label key={contact.provider_id} className="contact-row">
              <input
                type="checkbox"
                checked={selected.has(contact.provider_id)}
                onChange={() => toggle(contact.provider_id, contact.name)}
              />
              <LiveAvatar
                accountID={account.id}
                name={contact.name}
                url={contact.avatar_url}
                size="small"
              />
              <span>
                <strong>{contact.name}</strong>
                <small>
                  {contact.phone ||
                    contact.masked_phone ||
                    t(($) => $.chats.whatsappContact)}
                </small>
              </span>
            </label>
          ))
        )}
        {contacts.hasNextPage && (
          <Button
            className="load-more"
            busy={contacts.isFetchingNextPage}
            onClick={() => contacts.fetchNextPage()}
          >
            {t(($) => $.chats.moreContacts)}
          </Button>
        )}
      </div>
    </div>
  );
}
