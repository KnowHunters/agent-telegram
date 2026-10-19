import React from "react";
import {createRoot} from "react-dom/client";
import {Alert, Button, Card, Spinner} from "@heroui/react";

import "./styles.css";

import {AccessSetup} from "./components/AccessSetup";
import {ApiSettingsPanel} from "./components/ApiSettingsPanel";
import {AuthHeader} from "./components/Brand";
import {AuthScreen} from "./components/AuthScreens";
import {useAuthState} from "./hooks";
import {LanguageProvider, useI18n} from "./i18n";
import {AuthMode, AuthState} from "./types";

function descriptionFor(state: AuthState | undefined, t: ReturnType<typeof useI18n>["t"]) {
  switch (state?.mode) {
    case "qr":
      return t.headerDescriptionQR;
    case "setup":
      return t.headerDescriptionSetup;
    case "done":
      return t.headerDescriptionDone;
    default:
      return t.headerDescriptionLoading;
  }
}

function titleFor(mode: AuthMode | undefined, t: ReturnType<typeof useI18n>["t"]) {
  if (!mode) {
    return t.authentication;
  }
  if (mode === "done") {
    return t.doneTitle;
  }
  return t.authentication;
}

function App() {
  const {state, setState, loadError, retry} = useAuthState();
  const {t} = useI18n();
  const setupMode = state?.mode === "setup";

  return (
    <main className="auth-shell">
      <Card className={`auth-card ${setupMode ? "auth-card--setup" : ""}`} variant="default">
        <Card.Header className="card-header">
          <AuthHeader
            description={descriptionFor(state ?? undefined, t)}
            mode={state?.mode}
            title={titleFor(state?.mode, t)}
          />
        </Card.Header>

        <Card.Content className="auth-content">
          {loadError && (
            <Alert status="danger" role="alert">
              <Alert.Content>
                <Alert.Title>{t.connectionLost}</Alert.Title>
                <Alert.Description>{loadError}</Alert.Description>
              </Alert.Content>
              <Button size="sm" type="button" variant="secondary" onClick={() => void retry()}>
                {t.retry}
              </Button>
            </Alert>
          )}

          {state ? (
            state.mode === "setup" ? (
              <AccessSetup state={state} onState={setState} />
            ) : (
              <>
                <AuthScreen state={state} onState={setState} />
                {state.mode === "qr" && state.api.canEdit && (
                  <ApiSettingsPanel api={state.api} authError={state.error} onUpdated={setState} />
                )}
              </>
            )
          ) : (
            <div className="page-loading" aria-live="polite">
              <Spinner size="lg" />
              <span>{t.preparingAuthentication}</span>
            </div>
          )}
        </Card.Content>
      </Card>
    </main>
  );
}

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <LanguageProvider>
      <App />
    </LanguageProvider>
  </React.StrictMode>,
);
