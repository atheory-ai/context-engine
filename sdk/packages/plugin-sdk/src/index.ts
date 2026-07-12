export { definePlugin, setPluginDefinition } from "./define.js"
export { log, emit, createSubstrateClient, getConfig, nodeID, edgeID } from "./host.js"
export {
  walk,
  walkTopLevel,
  childByField,
  childrenByType,
  firstByType,
  hasChildType,
  fieldText,
  firstDescendantByType,
} from "./tree.js"
export type {
  SyntaxNode,
  Position,
  Node,
  Edge,
  NodeType,
  EdgeType,
  SourceClass,
  ExtractionResult,
  ConceptSeed,
  AnchorRef,
  Anchor,
  IR,
  Emission,
  ToolRequest,
  ToolResult,
  SubstrateQuery,
  SubstrateClient,
  LanguageDefinition,
  RoleDefinition,
  AnalyzerDefinition,
  ToolDefinition,
  PluginDefinition,
  IIRSeverity,
  IIRTarget,
  IIRExprPattern,
  IIRRuleWhen,
  IIRRuleRequire,
  IIRRule,
  IIRRulePack,
} from "./types.js"
export { IIRTypeUnknown } from "./iir.js"
export type {
  IIRVisibility,
  IIRParam,
  IIRReturn,
  IIRExpr,
  IIRBehaviorClause,
  IIRSideEffect,
  FunctionIntent,
  ExtractedFunction,
} from "./iir.js"
export { classifyEffect } from "./effects.js"
export type { EffectKind, EffectConfidence, EffectClassifierInput } from "./effects.js"
