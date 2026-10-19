import React, {createContext, useCallback, useContext, useEffect, useMemo, useState} from "react";

export type Language = "zh" | "en";

const STORAGE_KEY = "agent-telegram-auth-language";

type MessagesShape = {
  languageName: string;
  switchLanguage: string;
  authentication: string;
  headerDescriptionQR: string;
  headerDescriptionSetup: string;
  headerDescriptionDone: string;
  headerDescriptionLoading: string;
  stepOf: (step: number) => string;
  doneLabel: string;
  authenticationCompleteLabel: string;
  connectionLost: string;
  retry: string;
  preparingAuthentication: string;
  couldNotContinue: string;
  couldNotContinueFallback: string;
  codeRefreshAutomatic: string;
  refreshingQR: string;
  codeRefreshesIn: (time: string) => string;
  macOSKeychain: string;
  temporaryMemory: string;
  sessionSavedTo: string;
  sessionSavedProfile: string;
  apiCredentialsUnavailable: string;
  apiCredentialsHelp: string;
  scanInstructions: string;
  openTelegram: string;
  settingsDevices: string;
  settingsDevicesStrong: string;
  linkDevice: string;
  linkDeviceStrong: string;
  linkDeviceEnd: string;
  qrAlt: string;
  creatingQR: string;
  sessionStorageAttention: string;
  mockMode: string;
  mockModeHelp: string;
  simulateQR: string;
  oneTimeLink: string;
  doneTitle: string;
  doneDescription: string;
  advancedSettings: string;
  apiIDHash: string;
  defaultAPI: string;
  customAPI: string;
  currentCredentialsUnavailable: string;
  apiSettingsHelp: string;
  couldNotSaveAPI: string;
  apiSettingsFallback: string;
  appID: string;
  appHash: string;
  saving: string;
  apply: string;
  restoreDefaults: string;
  couldNotLoadChats: string;
  accessSettings: string;
  chooseAccess: string;
  chooseChat: string;
  couldNotSaveAccess: string;
  allCurrentFutureChats: string;
  selectedChats: (selected: number, total: number) => string;
  accessNotSelected: string;
  chatAccessOption: string;
  allChats: string;
  allChatsDescription: string;
  botsOnly: string;
  botsOnlyDescription: string;
  chooseManually: string;
  chooseManuallyDescription: string;
  agentAllChats: string;
  confirmationsRemain: string;
  searchChats: string;
  searchPlaceholder: string;
  chatType: string;
  loadingChats: string;
  selectedCount: (selected: number, total: number) => string;
  selectVisible: string;
  clearVisible: string;
  loadingChatList: string;
  noMatchingChats: string;
  readyToSave: string;
  saveAndFinish: string;
  all: string;
  people: string;
  groups: string;
  channels: string;
  bots: string;
};

const messages: Record<Language, MessagesShape> = {
  en: {
    languageName: "English",
    switchLanguage: "Switch to Chinese",
    authentication: "Telegram authentication",
    headerDescriptionQR: "Connect this computer to your account using the Telegram app.",
    headerDescriptionSetup: "Choose which chats the local agent can interact with.",
    headerDescriptionDone: "Telegram is connected and your local permissions are saved.",
    headerDescriptionLoading: "Preparing a secure local authentication page.",
    stepOf: (step: number) => `Step ${step} of 2`,
    doneLabel: "Done",
    authenticationCompleteLabel: "Authentication complete",
    connectionLost: "Connection lost",
    retry: "Retry",
    preparingAuthentication: "Preparing authentication…",
    couldNotContinue: "Could not continue",
    couldNotContinueFallback: "Could not continue.",
    codeRefreshAutomatic: "The code refreshes automatically",
    refreshingQR: "Refreshing the QR code…",
    codeRefreshesIn: (time: string) => `Code refreshes in ${time}`,
    macOSKeychain: "macOS Keychain",
    temporaryMemory: "temporary memory",
    sessionSavedTo: "Your session will be saved to",
    sessionSavedProfile: "under profile",
    apiCredentialsUnavailable: "Telegram API credentials are unavailable",
    apiCredentialsHelp: "Create an application at my.telegram.org with this Telegram account, then enter its API ID and Hash under Advanced settings.",
    scanInstructions: "How to scan the QR code",
    openTelegram: "Open Telegram on your phone",
    settingsDevices: "Go to",
    settingsDevicesStrong: "Settings → Devices",
    linkDevice: "Tap",
    linkDeviceStrong: "Link Desktop Device",
    linkDeviceEnd: "and point the camera at the code",
    qrAlt: "QR code for signing in to Telegram",
    creatingQR: "Creating a secure QR code…",
    sessionStorageAttention: "Session storage needs attention",
    mockMode: "Mock mode",
    mockModeHelp: "Continue to access setup without Telegram.",
    simulateQR: "Simulate QR scan",
    oneTimeLink: "This page is protected by a one-time link and closes after sign-in.",
    doneTitle: "Authentication complete",
    doneDescription: "Your session has been saved. You can close this page and return to the terminal.",
    advancedSettings: "Advanced settings",
    apiIDHash: "Telegram API ID and Hash",
    defaultAPI: "Default API",
    customAPI: "Custom API",
    currentCredentialsUnavailable: "The current API credentials cannot be used. Create an application at my.telegram.org, then enter its API ID and Hash.",
    apiSettingsHelp: "Most users do not need to change these settings. You can get custom credentials from my.telegram.org.",
    couldNotSaveAPI: "Could not save API settings",
    apiSettingsFallback: "Could not update the Telegram API settings.",
    appID: "App ID",
    appHash: "App Hash",
    saving: "Saving…",
    apply: "Apply",
    restoreDefaults: "Restore defaults",
    couldNotLoadChats: "Could not load your chats.",
    accessSettings: "Check your access settings",
    chooseAccess: "Choose an access option first.",
    chooseChat: "Choose at least one chat or allow access to all chats.",
    couldNotSaveAccess: "Could not save access settings.",
    allCurrentFutureChats: "All current and future chats",
    selectedChats: (selected: number, total: number) => `${selected} of ${total} chats`,
    accessNotSelected: "Access has not been selected",
    chatAccessOption: "Chat access option",
    allChats: "All chats",
    allChatsDescription: "Current and future chats",
    botsOnly: "Bots only",
    botsOnlyDescription: "No people, groups, or channels",
    chooseManually: "Choose manually",
    chooseManuallyDescription: "Create an exact chat allowlist",
    agentAllChats: "The agent can work with all chats",
    confirmationsRemain: "Destructive and paid actions will still require separate confirmation.",
    searchChats: "Search chats",
    searchPlaceholder: "Search by name or username",
    chatType: "Chat type",
    loadingChats: "Loading chats…",
    selectedCount: (selected: number, total: number) => `${selected} of ${total} selected`,
    selectVisible: "Select visible",
    clearVisible: "Clear visible",
    loadingChatList: "Loading your chat list…",
    noMatchingChats: "No chats match this search",
    readyToSave: "Ready to save",
    saveAndFinish: "Save and finish",
    all: "All",
    people: "People",
    groups: "Groups",
    channels: "Channels",
    bots: "Bots",
  },
  zh: {
    languageName: "中文",
    switchLanguage: "切换到英文",
    authentication: "Telegram 登录",
    headerDescriptionQR: "使用手机 Telegram 连接此设备与你的账号。",
    headerDescriptionSetup: "选择本地代理可操作的聊天范围。",
    headerDescriptionDone: "Telegram 已连接，本地权限已保存。",
    headerDescriptionLoading: "正在准备安全登录页。",
    stepOf: (step: number) => `第 ${step} 步，共 2 步`,
    doneLabel: "完成",
    authenticationCompleteLabel: "登录完成",
    connectionLost: "连接已断开",
    retry: "重试",
    preparingAuthentication: "正在准备登录…",
    couldNotContinue: "无法继续",
    couldNotContinueFallback: "无法继续。",
    codeRefreshAutomatic: "二维码会自动刷新",
    refreshingQR: "正在刷新二维码…",
    codeRefreshesIn: (time: string) => `二维码将在 ${time} 后刷新`,
    macOSKeychain: "macOS 钥匙串",
    temporaryMemory: "临时内存",
    sessionSavedTo: "会话将保存到",
    sessionSavedProfile: "配置名",
    apiCredentialsUnavailable: "Telegram API 凭据不可用",
    apiCredentialsHelp: "请使用当前 Telegram 账号在 my.telegram.org 创建应用，然后在“高级设置”中填写 API ID 和 Hash。",
    scanInstructions: "扫码登录说明",
    openTelegram: "在手机上打开 Telegram",
    settingsDevices: "前往",
    settingsDevicesStrong: "设置 → 设备",
    linkDevice: "点按",
    linkDeviceStrong: "关联桌面设备",
    linkDeviceEnd: "，将摄像头对准二维码",
    qrAlt: "Telegram 登录二维码",
    creatingQR: "正在生成安全二维码…",
    sessionStorageAttention: "会话存储需要处理",
    mockMode: "模拟模式",
    mockModeHelp: "无需 Telegram 即可继续测试授权设置。",
    simulateQR: "模拟扫码",
    oneTimeLink: "此页面受一次性链接保护，登录后自动关闭。",
    doneTitle: "登录完成",
    doneDescription: "会话已保存。你可以关闭此页面并返回终端。",
    advancedSettings: "高级设置",
    apiIDHash: "Telegram API ID 与 Hash",
    defaultAPI: "默认 API",
    customAPI: "自定义 API",
    currentCredentialsUnavailable: "当前 API 凭据无法使用。请在 my.telegram.org 创建应用，然后填写 API ID 和 Hash。",
    apiSettingsHelp: "大多数用户无需修改。自定义凭据可在 my.telegram.org 获取。",
    couldNotSaveAPI: "无法保存 API 设置",
    apiSettingsFallback: "无法更新 Telegram API 设置。",
    appID: "App ID",
    appHash: "App Hash",
    saving: "正在保存…",
    apply: "应用",
    restoreDefaults: "恢复默认",
    couldNotLoadChats: "无法加载聊天列表。",
    accessSettings: "请检查访问权限设置",
    chooseAccess: "请先选择一种访问方式。",
    chooseChat: "请至少选择一个聊天，或允许访问全部聊天。",
    couldNotSaveAccess: "无法保存访问权限。",
    allCurrentFutureChats: "当前及未来的全部聊天",
    selectedChats: (selected: number, total: number) => `已选 ${selected} / ${total} 个聊天`,
    accessNotSelected: "尚未选择访问范围",
    chatAccessOption: "聊天访问方式",
    allChats: "全部聊天",
    allChatsDescription: "当前及未来的聊天",
    botsOnly: "仅机器人",
    botsOnlyDescription: "不包括个人、群组和频道",
    chooseManually: "手动选择",
    chooseManuallyDescription: "创建精确的聊天白名单",
    agentAllChats: "代理可操作全部聊天",
    confirmationsRemain: "破坏性操作和付费操作仍需单独确认。",
    searchChats: "搜索聊天",
    searchPlaceholder: "按名称或用户名搜索",
    chatType: "聊天类型",
    loadingChats: "正在加载聊天…",
    selectedCount: (selected: number, total: number) => `已选 ${selected} / ${total}`,
    selectVisible: "选择当前结果",
    clearVisible: "清除当前结果",
    loadingChatList: "正在加载聊天列表…",
    noMatchingChats: "没有符合条件的聊天",
    readyToSave: "等待保存",
    saveAndFinish: "保存并完成",
    all: "全部",
    people: "个人",
    groups: "群组",
    channels: "频道",
    bots: "机器人",
  },
};

export type Messages = MessagesShape;

type LanguageContextValue = {
  language: Language;
  setLanguage: (language: Language) => void;
  toggleLanguage: () => void;
  t: Messages;
};

const LanguageContext = createContext<LanguageContextValue | null>(null);

function initialLanguage(): Language {
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    if (stored === "zh" || stored === "en") {
      return stored;
    }
  } catch {
    // Storage can be disabled; browser language remains a safe fallback.
  }
  return window.navigator.language.toLowerCase().startsWith("zh") ? "zh" : "en";
}

export function LanguageProvider({children}: {children: React.ReactNode}) {
  const [language, setLanguageState] = useState<Language>(initialLanguage);

  useEffect(() => {
    document.documentElement.lang = language === "zh" ? "zh-CN" : "en";
    document.title = language === "zh" ? "Telegram 登录" : "Telegram login";
  }, [language]);

  const setLanguage = useCallback((next: Language) => {
    setLanguageState(next);
    try {
      window.localStorage.setItem(STORAGE_KEY, next);
    } catch {
      // The current page still changes language when storage is unavailable.
    }
  }, []);

  const value = useMemo<LanguageContextValue>(() => ({
    language,
    setLanguage,
    toggleLanguage: () => setLanguage(language === "zh" ? "en" : "zh"),
    t: messages[language],
  }), [language, setLanguage]);

  return <LanguageContext.Provider value={value}>{children}</LanguageContext.Provider>;
}

export function useI18n() {
  const context = useContext(LanguageContext);
  if (!context) {
    throw new Error("useI18n must be used inside LanguageProvider");
  }
  return context;
}

