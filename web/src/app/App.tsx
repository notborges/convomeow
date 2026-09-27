import { Chat01Icon } from "@hugeicons/core-free-icons";
import { HugeiconsIcon } from "@hugeicons/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { lazy, Suspense, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Navigate,
  Route,
  Routes,
  useMatch,
  useNavigate,
  useParams,
} from "react-router";
import { api } from "../api/client";
import { keys } from "../api/queries";
import type { Account } from "../api/types";
import { AccountRail } from "../features/accounts/AccountRail";
import { PairingDialog } from "../features/accounts/PairingDialog";
import { Login } from "../features/auth/Login";
import { ChatSidebar } from "../features/chats/ChatSidebar";
import { ChatThread } from "../features/chats/ChatThread";
import { Button } from "../ui/Button";
import { EmptyState } from "../ui/EmptyState";
import { Loading } from "../ui/Loading";
import { useRealtime } from "./useRealtime";

const DesignSystem = import.meta.env.DEV
  ? lazy(() => import("../ui/catalog/DesignSystem"))
  : undefined;

export function App() {
  if (DesignSystem && window.location.pathname === "/app/design-system") {
    return (
      <Suspense fallback={<Loading />}>
        <DesignSystem />
      </Suspense>
    );
  }
  return <MessagingApp />;
}

function MessagingApp() {
  const { t } = useTranslation();

  const [authenticated, setAuthenticated] = useState<boolean | null>(null);
  const queryClient = useQueryClient();
  const route = useMatch("/accounts/:accountID/*");
  const realtime = useRealtime(authenticated === true, route?.params.accountID);

  useEffect(() => {
    let active = true;
    api
      .session()
      .then((session) => {
        if (active) setAuthenticated(session.authenticated);
      })
      .catch(() => {
        if (active) setAuthenticated(false);
      });
    const unauthorized = () => {
      queryClient.clear();
      setAuthenticated(false);
    };
    window.addEventListener("convomeow:unauthorized", unauthorized);
    return () => {
      active = false;
      window.removeEventListener("convomeow:unauthorized", unauthorized);
    };
  }, [queryClient]);

  if (authenticated === null)
    return (
      <div className="boot">
        <Loading label={t(($) => $.auth.opening)} />
      </div>
    );
  if (!authenticated) return <Login onLogin={() => setAuthenticated(true)} />;

  return (
    <>
      {realtime !== "connected" && (
        <div className="realtime-status" role="status">
          {realtime === "connecting"
            ? t(($) => $.realtime.connecting)
            : t(($) => $.realtime.reconnecting)}
        </div>
      )}
      <Routes>
        <Route
          path="/"
          element={
            <Inbox
              onLogout={() => {
                queryClient.clear();
                setAuthenticated(false);
              }}
            />
          }
        />
        <Route
          path="/accounts/:accountID/chats"
          element={
            <Inbox
              onLogout={() => {
                queryClient.clear();
                setAuthenticated(false);
              }}
            />
          }
        />
        <Route
          path="/accounts/:accountID/chats/:conversationID"
          element={
            <Inbox
              onLogout={() => {
                queryClient.clear();
                setAuthenticated(false);
              }}
            />
          }
        />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </>
  );
}

function Inbox({ onLogout }: { onLogout: () => void }) {
  const { t } = useTranslation();

  const { accountID, conversationID } = useParams();
  const navigate = useNavigate();
  const [pairing, setPairing] = useState(false);
  const [pairAttempts, setPairAttempts] = useState<Record<string, string>>({});
  const accountsQuery = useQuery({
    queryKey: keys.accounts,
    queryFn: api.accounts,
  });
  const accounts = accountsQuery.data?.items ?? [];
  const selected: Account | undefined = accounts.find(
    (account) => account.id === accountID,
  );

  useEffect(() => {
    if (accountsQuery.isSuccess && accounts.length > 0 && !selected) {
      navigate(`/accounts/${accounts[0].id}/chats`, { replace: true });
    }
  }, [accountsQuery.isSuccess, accounts, selected, navigate]);

  async function logout() {
    await api.logout();
    onLogout();
  }

  return (
    <div
      className={`workspace ${conversationID ? "workspace--thread-open" : ""}`}
    >
      <AccountRail
        accounts={accounts}
        selectedID={selected?.id}
        onSelect={(id) => navigate(`/accounts/${id}/chats`)}
        onCreated={(id) => navigate(`/accounts/${id}/chats`)}
        onLogout={logout}
      />
      {accountsQuery.isPending ? (
        <div className="sidebar">
          <Loading label={t(($) => $.accounts.loading)} />
        </div>
      ) : accountsQuery.isError ? (
        <div className="sidebar">
          <EmptyState title={t(($) => $.accounts.loadError)}>
            <Button type="button" onClick={() => accountsQuery.refetch()}>
              {t(($) => $.common.retry)}
            </Button>
          </EmptyState>
        </div>
      ) : selected ? (
        <ChatSidebar
          key={selected.id}
          account={selected}
          activeConversationID={conversationID}
          onOpen={(id) => navigate(`/accounts/${selected.id}/chats/${id}`)}
          onPair={() => setPairing(true)}
        />
      ) : (
        <div className="sidebar">
          <EmptyState
            icon={<HugeiconsIcon icon={Chat01Icon} size={30} />}
            title={t(($) => $.accounts.welcome)}
            description={t(($) => $.accounts.setup)}
          />
        </div>
      )}
      {selected && conversationID ? (
        <ChatThread
          key={conversationID}
          account={selected}
          conversationID={conversationID}
          onBack={() => navigate(`/accounts/${selected.id}/chats`)}
        />
      ) : (
        <main className="thread thread--empty">
          <div className="welcome">
            <div className="welcome__icon">
              <HugeiconsIcon icon={Chat01Icon} size={36} />
            </div>
            <h1>{t(($) => $.chats.select)}</h1>
            <p>
              {selected
                ? t(($) => $.chats.selectDescription)
                : t(($) => $.chats.addFirst)}
            </p>
          </div>
        </main>
      )}
      {selected && pairing && (
        <PairingDialog
          key={selected.id}
          account={selected}
          existingAttemptID={pairAttempts[selected.id]}
          onStarted={(id) =>
            setPairAttempts((attempts) => ({ ...attempts, [selected.id]: id }))
          }
          onClose={() => setPairing(false)}
        />
      )}
    </div>
  );
}
