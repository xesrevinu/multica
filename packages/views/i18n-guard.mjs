import ts from "@typescript/typescript6";

// User-visible copy reaches the screen through JSX text, a copy-bearing
// attribute, or a toast argument. The oxlint plugin reports these messages;
// this module is the check both the plugin and the fixture test run.

const COPY_ATTRIBUTES = new Set(["placeholder", "title", "aria-label"]);

const ATTRIBUTE_MESSAGE =
  "User-visible text in placeholder/title/aria-label must come from useT() — a screen reader reads aria-label the way a sighted user reads a JSX child. Exempt technical values (a CLI command, a token prefix) with an inline oxlint-disable and a reason.";

const TOAST_MESSAGE =
  "Toast copy must come from useT(). Pass a translated string, not a literal — including the fallback half of a `err.message : \"…\"` ternary.";

const JSX_TEXT_MESSAGE =
  "User-visible JSX text must come from useT(). Raw strings in JSX children are a build error.";

function isCopyText(text) {
  return typeof text === "string" && /[A-Za-z]/.test(text) && !/^https?:/.test(text);
}

function templateChunks(node) {
  if (ts.isNoSubstitutionTemplateLiteral(node)) return [node.text];
  if (!ts.isTemplateExpression(node)) return null;
  const chunks = [node.head.text];
  for (const span of node.templateSpans) chunks.push(span.literal.text);
  return chunks;
}

function isCopyNode(node) {
  if (ts.isStringLiteralLike(node)) return isCopyText(node.text);
  const chunks = templateChunks(node);
  return chunks !== null && chunks.some(isCopyText);
}

function attributeIsCopy(initializer) {
  if (!initializer) return false;
  if (ts.isStringLiteralLike(initializer)) return isCopyText(initializer.text);
  if (!ts.isJsxExpression(initializer)) return false;
  return isCopyNode(initializer.expression);
}

function toastArgumentIsCopy(argument) {
  if (!argument) return false;
  if (isCopyNode(argument)) return true;
  if (ts.isConditionalExpression(argument)) {
    return isCopyNode(argument.whenTrue) || isCopyNode(argument.whenFalse);
  }
  if (ts.isBinaryExpression(argument)) {
    const op = argument.operatorToken.kind;
    const logical =
      op === ts.SyntaxKind.BarBarToken ||
      op === ts.SyntaxKind.AmpersandAmpersandToken ||
      op === ts.SyntaxKind.QuestionQuestionToken;
    if (!logical) return false;
    return isCopyNode(argument.left) || isCopyNode(argument.right);
  }
  return false;
}

function isToastCall(node) {
  if (!ts.isCallExpression(node)) return false;
  const callee = node.expression;
  if (ts.isIdentifier(callee)) return callee.text === "toast";
  return ts.isPropertyAccessExpression(callee) && ts.isIdentifier(callee.expression) && callee.expression.text === "toast";
}

export function messagesForSource(code) {
  const source = ts.createSourceFile("fixture.tsx", code, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const messages = [];
  function visit(node) {
    if (ts.isJsxText(node) && isCopyText(node.getText(source).trim())) {
      messages.push(JSX_TEXT_MESSAGE);
    }
    if (ts.isJsxAttribute(node) && COPY_ATTRIBUTES.has(node.name.getText(source)) && attributeIsCopy(node.initializer)) {
      messages.push(ATTRIBUTE_MESSAGE);
    }
    if (isToastCall(node) && toastArgumentIsCopy(node.arguments[0])) {
      messages.push(TOAST_MESSAGE);
    }
    ts.forEachChild(node, visit);
  }
  visit(source);
  return messages;
}
