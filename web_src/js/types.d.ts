interface Window {
  __webpack_public_path__: string;
  config?: {
    appUrl: string;
    appSubUrl: string;
    assetUrlPrefix: string;
    pageData: PageData;
    i18n: Record<string, string>;
    customEmojis: Set<string>;
    mentionValues: MentionValue[];
  }
}

interface PageData {
  PLURALSTRINGS_LANG: Record<string, string[]>;
  PLURAL_RULE_LANG: number;
  DATETIMESTRINGS: Record<string, string>;

  [key: string]: unknown; // TODO: remove this after we've enumerated all pageData entries
}

interface MentionValue {
  key: string;
  value: string;
  name: string;
  fullname: string;
  avatar: string;
}

type CodeMirrorLanguage = typeof import('@codemirror/language');
type CodeMirrorSearch = typeof import('@codemirror/search');
type CodeMirrorState = typeof import('@codemirror/state');
type CodeMirrorView = typeof import('@codemirror/view');

declare module '*.vue' {
  import Vue from 'vue';
  export default Vue;
}

// until the plugin defines its own types, or we finish removing jquery:
declare module 'eslint-plugin-no-jquery' {
  import type {Plugin} from '@eslint/core';
  const plugin: Plugin;
  export = plugin;
}

// until https://github.com/freaktechnik/eslint-plugin-array-func/issues/492 is resolved:
declare module 'eslint-plugin-array-func' {
  // no-duplicate-imports: false-positive, moving this import to root makes this file a "module" and breaks the global-ness of the other declarations.
  // eslint-disable-next-line no-duplicate-imports
  import type {Plugin} from '@eslint/core';
  const plugin: Plugin;
  export default plugin;
}

// until https://github.com/dustinspecker/eslint-plugin-no-use-extend-native/issues/156 is resolved:
declare module 'eslint-plugin-no-use-extend-native' {
  import type {Plugin} from '@eslint/core'; // eslint-disable-line no-duplicate-imports
  const plugin: Plugin;
  export default plugin;
}
