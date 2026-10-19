import React, {useState} from "react";
import {Alert, Button, Spinner} from "@heroui/react";

import {postAuthState} from "../api";
import {useCountdown} from "../hooks";
import {Messages, useI18n} from "../i18n";
import {AuthSession, AuthState} from "../types";
import {CheckIcon, LockIcon} from "./Brand";

type ScreenProps = {
  state: AuthState;
  onState: (state: AuthState) => void;
};

function ErrorAlert({message, t}: {message: string; t: Messages}) {
  if (!message) {
    return null;
  }
  return (
    <Alert className="state-alert" status="danger" role="alert">
      <Alert.Content>
        <Alert.Title>{t.couldNotContinue}</Alert.Title>
        <Alert.Description>{message}</Alert.Description>
      </Alert.Content>
    </Alert>
  );
}

function useAuthAction(onState: (state: AuthState) => void, t: Messages) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const run = async (path: string, payload: unknown) => {
    setBusy(true);
    setError("");
    try {
      const result = await postAuthState(path, payload);
      onState(result.state);
      if (!result.ok) {
        setError(result.state.error || t.couldNotContinueFallback);
        return false;
      }
      return true;
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : t.couldNotContinueFallback);
      return false;
    } finally {
      setBusy(false);
    }
  };

  return {busy, error, run};
}

function formatCountdown(seconds: number | null, t: Messages) {
  if (seconds === null) {
    return t.codeRefreshAutomatic;
  }
  if (seconds <= 0) {
    return t.refreshingQR;
  }
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  return t.codeRefreshesIn(`${minutes}:${rest.toString().padStart(2, "0")}`);
}

function sessionProviderLabel(provider: string, t: Messages) {
  switch (provider) {
    case "keychain":
      return t.macOSKeychain;
    case "memory":
      return t.temporaryMemory;
    default:
      return provider;
  }
}

function SessionSaveNote({session, t}: {session?: AuthSession; t: Messages}) {
  if (!session?.saveByDefault) {
    return null;
  }
  const provider = sessionProviderLabel(session.provider, t);
  return (
    <div className="session-save-note">
      <LockIcon />
      <span>
        {t.sessionSavedTo} <strong>{provider}</strong>, {t.sessionSavedProfile} <strong>{session.profile}</strong>
        {t.sessionSavedPunctuation}
      </span>
    </div>
  );
}

function APIErrorHelp({message, t}: {message: string; t: Messages}) {
  if (!message.includes("API_ID_PUBLISHED_FLOOD") && !message.includes("API_ID_INVALID")) {
    return null;
  }
  return (
    <Alert className="state-alert" status="warning" role="status">
      <Alert.Content>
        <Alert.Title>{t.apiCredentialsUnavailable}</Alert.Title>
        <Alert.Description>{t.apiCredentialsHelp}</Alert.Description>
      </Alert.Content>
    </Alert>
  );
}

function QRScreen({state, onState}: ScreenProps) {
  const {t} = useI18n();
  const countdown = useCountdown(state.expires);
  const {busy, error, run} = useAuthAction(onState, t);
  return (
    <section className="auth-screen qr-screen">
      <ol className="qr-instructions" aria-label={t.scanInstructions}>
        <li><span>1</span><p>{t.openTelegram}</p></li>
        <li><span>2</span><p>{t.settingsDevices} <strong>{t.settingsDevicesStrong}</strong></p></li>
        <li>
          <span>3</span>
          <p>
            {t.linkDevice} <strong>{t.linkDeviceStrong}</strong> {t.linkDeviceEnd}
          </p>
        </li>
      </ol>

      <div className="qr-stage" aria-live="polite" aria-busy={!state.qrImage}>
        {state.qrImage ? (
          <img
            alt={t.qrAlt}
            className="qr-image"
            key={state.expires || state.qrImage.slice(-24)}
            src={state.qrImage}
          />
        ) : (
          <div className="qr-placeholder">
            <Spinner size="lg" />
            <span>{t.creatingQR}</span>
          </div>
        )}
      </div>
      <p className="qr-expiry" aria-live="polite">{formatCountdown(countdown, t)}</p>

      <ErrorAlert message={error || state.error || ""} t={t} />
      <APIErrorHelp message={state.error || ""} t={t} />

      {state.session?.error && !state.error && !error && (
        <Alert className="state-alert" status="warning" role="status">
          <Alert.Content>
            <Alert.Title>{t.sessionStorageAttention}</Alert.Title>
            <Alert.Description>{state.session.error}</Alert.Description>
          </Alert.Content>
        </Alert>
      )}

      {state.mock?.enabled && (
        <div className="mock-panel">
          <div>
            <strong>{t.mockMode}</strong>
            <span>{t.mockModeHelp}</span>
          </div>
          <Button isDisabled={busy} size="sm" type="button" onClick={() => void run("/auth/mock/advance", {action: "qr_scan"})}>
            {t.simulateQR}
          </Button>
        </div>
      )}

      <SessionSaveNote session={state.session} t={t} />
      <div className="trust-note">
        <LockIcon />
        <span>{t.oneTimeLink}</span>
      </div>
    </section>
  );
}

function DoneScreen() {
  const {t} = useI18n();
  return (
    <section className="auth-screen done-screen" aria-live="polite">
      <div className="success-mark"><CheckIcon /></div>
      <div>
        <h2>{t.doneTitle}</h2>
        <p>{t.doneDescription}</p>
      </div>
    </section>
  );
}

export function AuthScreen(props: ScreenProps) {
  switch (props.state.mode) {
    case "qr":
      return <QRScreen {...props} />;
    case "done":
      return <DoneScreen />;
    default:
      return null;
  }
}
