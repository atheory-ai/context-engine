// Package vocabulary defines the controlled semantic tags that bridge a
// natural-language request, graph-backed resolution, and framework policies.
// Tags are intentionally facts about an implementation context, not snippets
// of source or a substitute for an observed graph relationship.
package vocabulary

import (
	"fmt"
	"sort"
	"strings"
)

// Tag is a stable semantic claim kind. A tag becomes a SemanticPlan claim with
// provenance; policies select tags but may not manufacture them.
type Tag string

const (
	OperationMutate            Tag = "operation.mutate"
	OperationCartModify        Tag = "operation.cart.modify"
	OperationCheckoutValidate  Tag = "operation.checkout.validate"
	OperationBlockRegister     Tag = "operation.block.register"
	SurfaceWPAdmin             Tag = "surface.wordpress.admin"
	SurfaceWPRequest           Tag = "surface.wordpress.request"
	SurfaceWPRest              Tag = "surface.wordpress.rest"
	SurfaceWooCheckout         Tag = "surface.woocommerce.checkout"
	SurfaceWooStoreAPI         Tag = "surface.woocommerce.store_api"
	SurfaceGutenbergEditor     Tag = "surface.gutenberg.editor"
	InputUserControlled        Tag = "input.user_controlled"
	OutputHTML                 Tag = "output.html"
	OutputUserFacingText       Tag = "output.user_facing_text"
	EffectDatabaseQuery        Tag = "effect.database.query"
	EffectCartMutation         Tag = "effect.cart.mutation"
	EffectPersistedContent     Tag = "effect.persisted_content"
	ContextWordPressPlugin     Tag = "context.wordpress.plugin"
	ContextWooCommerce         Tag = "context.woocommerce"
	ContextWooCommerceCart     Tag = "context.woocommerce.cart"
	ContextWooCommerceCheckout Tag = "context.woocommerce.checkout"
	ContextGutenberg           Tag = "context.gutenberg"
	ContextGutenbergBlock      Tag = "context.gutenberg.block"
	ContextGutenbergPackage    Tag = "context.gutenberg.package"
)

var allowed = map[Tag]struct{}{
	OperationMutate: {}, OperationCartModify: {}, OperationCheckoutValidate: {}, OperationBlockRegister: {},
	SurfaceWPAdmin: {}, SurfaceWPRequest: {}, SurfaceWPRest: {}, SurfaceWooCheckout: {}, SurfaceWooStoreAPI: {}, SurfaceGutenbergEditor: {},
	InputUserControlled: {}, OutputHTML: {}, OutputUserFacingText: {}, EffectDatabaseQuery: {}, EffectCartMutation: {}, EffectPersistedContent: {},
	ContextWordPressPlugin: {}, ContextWooCommerce: {}, ContextWooCommerceCart: {}, ContextWooCommerceCheckout: {}, ContextGutenberg: {}, ContextGutenbergBlock: {}, ContextGutenbergPackage: {},
}

// Normalize accepts only known tags, trims/deduplicates them, and returns
// deterministic lexical order. Unknown tags are rejected rather than silently
// becoming policy triggers.
func Normalize(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := allowed[Tag(value)]; !ok {
			return nil, fmt.Errorf("unknown semantic tag %q", value)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}

// Tags returns the controlled vocabulary for documentation, prompts, and UI.
func Tags() []string {
	out := make([]string, 0, len(allowed))
	for tag := range allowed {
		out = append(out, string(tag))
	}
	sort.Strings(out)
	return out
}
