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
    assert.equal(module.usesOpenRouterCreditPricing({ cost_source: 'model_pricing' }), false);
    assert.equal(module.usesOpenRouterCreditPricing({}), false);
});

test('costSourceTooltip explains OpenRouter credit costs', () => {
    const module = createUsageModule();

    assert.equal(
        module.costSourceTooltip({ cost_source: 'openrouter_credits' }),
        'Costs from OpenRouter USD-based credits.'
    );
    assert.equal(module.costSourceTooltip({ cost_source: 'model_pricing' }), '');
});
