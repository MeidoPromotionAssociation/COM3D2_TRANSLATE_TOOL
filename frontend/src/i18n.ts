import i18n from "i18next";
import {initReactI18next} from "react-i18next";
import en from "./locales/en.json";
import zh from "./locales/zh.json";

const languageStorageKey = "com3d2-translate-tool.language";

function getStoredLanguage() {
    try {
        return window.localStorage.getItem(languageStorageKey);
    } catch {
        return null;
    }
}

function resolveInitialLanguage() {
    const stored = getStoredLanguage();
    if (stored === "en" || stored === "zh") {
        return stored;
    }

    if (typeof navigator !== "undefined" && navigator.language.toLowerCase().startsWith("zh")) {
        return "zh";
    }

    return "en";
}

void i18n
    .use(initReactI18next)
    .init({
        resources: {
            en: {translation: en},
            zh: {translation: zh},
        },
        lng: resolveInitialLanguage(),
        fallbackLng: "en",
        interpolation: {
            escapeValue: false,
        },
    });

export function persistLanguage(language: string) {
    try {
        window.localStorage.setItem(languageStorageKey, language);
    } catch {
        // Ignore storage failures in embedded environments.
    }
}

export default i18n;
