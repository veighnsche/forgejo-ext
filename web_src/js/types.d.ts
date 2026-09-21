interface Window {
  __webpack_public_path__: string;
  config?: {
    appUrl: string;
    appSubUrl: string;
    assetUrlPrefix: string;
    pageData: Record<string, unknown>;
    i18n: Record<string, string>;
    customEmojis: Set<string>;
    mentionValues: MentionValue[];
  }
}

interface MentionValue {
  key: string;
  value: string;
  name: string;
  fullname: string;
  avatar: string;
}

declare module '*.vue' {
  import Vue from 'vue';
  export default Vue;
}

type CodeMirrorLanguage = typeof import('@codemirror/language');
type CodeMirrorSearch = typeof import('@codemirror/search');
type CodeMirrorState = typeof import('@codemirror/state');
type CodeMirrorView = typeof import('@codemirror/view');
