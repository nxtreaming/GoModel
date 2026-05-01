const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function loadUsageModuleFactory(overrides = {}) {
    const source = fs.readFileSync(path.join(__dirname, 'usage.js'), 'utf8');
    const window = {
        ...(overrides.window || {})
    };
    const context = {
        console,
        ...overrides,
        window
    };
    vm.createContext(context);
    vm.runInContext(source, context);
    return context.window.dashboardUsageModule;
}

function createUsageModule(overrides) {
    const factory = loadUsageModuleFactory(overrides);
    return factory();
}

test('usesOpenRouterCreditPricing detects OpenRouter credit cost source', () => {
    const module = createUsageModule();

    assert.equal(module.usesOpenRouterCreditPricing({ cost_source: 'openrouter_credits' }), true);
    assert.equal(module.usesOpenRouterCreditPricing({ cost_source: 'xai_cost_in_usd_ticks' }), false);
    assert.equal(module.usesOpenRouterCreditPricing({ cost_source: 'model_pricing' }), false);
    assert.equal(module.usesOpenRouterCreditPricing({}), false);
});

test('usesResponseCostPricing detects provider-reported costs', () => {
    const module = createUsageModule();

    assert.equal(module.usesResponseCostPricing({ cost_source: 'openrouter_credits' }), true);
    assert.equal(module.usesResponseCostPricing({ cost_source: 'xai_cost_in_usd_ticks' }), true);
    assert.equal(module.usesResponseCostPricing({ cost_source: 'model_pricing' }), false);
    assert.equal(module.usesResponseCostPricing({}), false);
});

test('costSourceTooltip explains provider-reported costs', () => {
    const module = createUsageModule();

    assert.equal(
        module.costSourceTooltip({ cost_source: 'openrouter_credits' }),
        'Costs from OpenRouter USD-based credits.'
    );
    assert.equal(
        module.costSourceTooltip({ cost_source: 'xai_cost_in_usd_ticks' }),
        'Costs from xAI usage.cost_in_usd_ticks.'
    );
    assert.equal(module.costSourceTooltip({ cost_source: 'model_pricing' }), '');
});
