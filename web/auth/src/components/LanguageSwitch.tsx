import React from "react";

import {useI18n} from "../i18n";

export function LanguageSwitch() {
  const {language, setLanguage, t} = useI18n();

  return (
    <div className="language-switch" role="group" aria-label={t.switchLanguage}>
      <button
        aria-pressed={language === "zh"}
        className={language === "zh" ? "is-active" : ""}
        type="button"
        onClick={() => setLanguage("zh")}
      >
        中文
      </button>
      <button
        aria-pressed={language === "en"}
        className={language === "en" ? "is-active" : ""}
        type="button"
        onClick={() => setLanguage("en")}
      >
        EN
      </button>
    </div>
  );
}
