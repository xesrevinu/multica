import { messagesForSource } from "./i18n-guard.mjs";

// Loaded by packages/views/.oxlintrc.json. Not type-aware: the check reparses
// the file with the TypeScript 6 API, which is the same function the fixture
// test calls. Oxlint does not run this until lint is turned back on.

function reportKind(prefix) {
  return {
    create(context) {
      return {
        Program(node) {
          const code = context.sourceCode.getText();
          for (const message of messagesForSource(code)) {
            if (message.startsWith(prefix)) context.report({ node, message });
          }
        },
      };
    },
  };
}

const plugin = {
  meta: { name: "multica-i18n" },
  rules: {
    "no-literal-jsx-text": reportKind("User-visible JSX text"),
    "no-untranslated-attribute": reportKind("User-visible text in placeholder"),
    "no-untranslated-toast": reportKind("Toast copy"),
  },
};

export default plugin;
