<script lang="ts">
	import { onMount } from 'svelte';
	import { listOrganizations, getUsage, createCheckoutSession, createPortalSession, type Organization, type UsageStats } from '$lib/api';
	import { ArrowLeft, Check, Zap } from 'lucide-svelte';

	let organizations: Organization[] = [];
	let selectedOrg: Organization | null = null;
	let usage: UsageStats | null = null;
	let loading = true;
	let processingUpgrade = false;

	const plans = [
		{
			name: 'Free',
			tier: 'free',
			price: '$0',
			period: 'forever',
			features: ['10 analyses per month', '1 organization', 'Community support'],
			cta: 'Current Plan'
		},
		{
			name: 'Team',
			tier: 'team',
			price: '$39',
			period: 'per user/month',
			features: ['1,000 analyses per month', 'Unlimited repositories', 'Team collaboration', 'Priority support', '180-day history'],
			cta: 'Upgrade',
			popular: true
		},
		{
			name: 'Enterprise',
			tier: 'enterprise',
			price: 'Custom',
			period: 'contact us',
			features: ['Unlimited analyses', 'SSO & SAML', 'Dedicated support', 'Custom integrations', 'SLA guarantee'],
			cta: 'Contact Sales'
		}
	];

	onMount(async () => {
		await loadData();
	});

	async function loadData() {
		loading = true;
		try {
			const result = await listOrganizations();
			organizations = result.organizations || [];
			if (organizations.length > 0) {
				selectedOrg = organizations[0];
				usage = await getUsage(selectedOrg.id);
			}
		} catch (error) {
			console.error('Failed to load data:', error);
		} finally {
			loading = false;
		}
	}

	async function handleUpgrade(tier: string) {
		if (!selectedOrg || processingUpgrade) return;

		processingUpgrade = true;
		try {
			const result = await createCheckoutSession(
				selectedOrg.id,
				tier,
				`${window.location.origin}/app/settings/billing?success=true`,
				`${window.location.origin}/app/settings/billing`
			);
			window.location.href = result.checkout_url;
		} catch (error) {
			console.error('Failed to create checkout session:', error);
			processingUpgrade = false;
		}
	}

	async function handleManageBilling() {
		if (!selectedOrg) return;

		try {
			const result = await createPortalSession(
				selectedOrg.id,
				`${window.location.origin}/app/settings/billing`
			);
			window.location.href = result.portal_url;
		} catch (error) {
			console.error('Failed to create portal session:', error);
		}
	}

	function isCurrentPlan(tier: string) {
		return selectedOrg?.tier === tier;
	}
</script>

<svelte:head>
	<title>Billing - Heisenberg</title>
</svelte:head>

<div class="space-y-6">
	<div class="flex items-center gap-4">
		<a
			href="/app/settings"
			class="p-2 rounded-md hover:bg-accent transition-colors"
		>
			<ArrowLeft class="h-5 w-5" />
		</a>
		<div>
			<h1 class="text-2xl font-bold">Billing & Usage</h1>
			<p class="text-muted-foreground">Manage your subscription</p>
		</div>
	</div>

	{#if loading}
		<div class="flex items-center justify-center py-12">
			<div class="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
		</div>
	{:else}
		<!-- Current Usage -->
		{#if usage}
			<div class="p-6 rounded-lg border border-border bg-card">
				<h2 class="font-semibold mb-4">Current Usage</h2>
				<div class="grid md:grid-cols-3 gap-4">
					<div>
						<p class="text-sm text-muted-foreground">Analyses Used</p>
						<p class="text-2xl font-bold">
							{usage.used_this_month}
							<span class="text-sm text-muted-foreground font-normal">
								/ {usage.limit === -1 ? 'Unlimited' : usage.limit}
							</span>
						</p>
					</div>
					<div>
						<p class="text-sm text-muted-foreground">Current Plan</p>
						<p class="text-2xl font-bold capitalize">{usage.tier}</p>
					</div>
					<div>
						<p class="text-sm text-muted-foreground">Resets On</p>
						<p class="text-2xl font-bold">
							{new Date(usage.reset_date).toLocaleDateString()}
						</p>
					</div>
				</div>

				{#if usage.limit !== -1}
					<div class="mt-4">
						<div class="w-full bg-muted rounded-full h-2">
							<div
								class="bg-primary rounded-full h-2 transition-all"
								style="width: {Math.min((usage.used_this_month / usage.limit) * 100, 100)}%"
							></div>
						</div>
					</div>
				{/if}
			</div>
		{/if}

		<!-- Manage Subscription -->
		{#if selectedOrg?.stripe_customer_id}
			<div class="p-6 rounded-lg border border-border bg-card">
				<h2 class="font-semibold mb-2">Manage Subscription</h2>
				<p class="text-sm text-muted-foreground mb-4">
					Update payment method, view invoices, or cancel your subscription
				</p>
				<button
					onclick={handleManageBilling}
					class="px-4 py-2 text-sm font-medium border border-border rounded-md hover:bg-accent transition-colors"
				>
					Open Billing Portal
				</button>
			</div>
		{/if}

		<!-- Plans -->
		<div>
			<h2 class="font-semibold mb-4">Plans</h2>
			<div class="grid md:grid-cols-3 gap-4">
				{#each plans as plan}
					<div
						class="relative p-6 rounded-lg border bg-card {plan.popular
							? 'border-primary'
							: 'border-border'}"
					>
						{#if plan.popular}
							<div class="absolute -top-3 left-1/2 -translate-x-1/2">
								<span class="px-3 py-1 text-xs font-medium bg-primary text-primary-foreground rounded-full">
									Popular
								</span>
							</div>
						{/if}

						<h3 class="text-lg font-semibold">{plan.name}</h3>
						<div class="mt-2 mb-4">
							<span class="text-3xl font-bold">{plan.price}</span>
							<span class="text-muted-foreground">/{plan.period}</span>
						</div>

						<ul class="space-y-2 mb-6">
							{#each plan.features as feature}
								<li class="flex items-center gap-2 text-sm">
									<Check class="h-4 w-4 text-primary" />
									{feature}
								</li>
							{/each}
						</ul>

						{#if isCurrentPlan(plan.tier)}
							<button
								disabled
								class="w-full px-4 py-2 text-sm font-medium border border-border rounded-md bg-muted text-muted-foreground"
							>
								Current Plan
							</button>
						{:else if plan.tier === 'enterprise'}
							<a
								href="mailto:sales@heisenberg.dev"
								class="block w-full px-4 py-2 text-sm font-medium text-center border border-border rounded-md hover:bg-accent transition-colors"
							>
								Contact Sales
							</a>
						{:else}
							<button
								onclick={() => handleUpgrade(plan.tier)}
								disabled={processingUpgrade}
								class="w-full px-4 py-2 text-sm font-medium bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors disabled:opacity-50 flex items-center justify-center gap-2"
							>
								{#if processingUpgrade}
									<div class="animate-spin rounded-full h-4 w-4 border-b-2 border-white"></div>
								{:else}
									<Zap class="h-4 w-4" />
								{/if}
								{plan.cta}
							</button>
						{/if}
					</div>
				{/each}
			</div>
		</div>
	{/if}
</div>
