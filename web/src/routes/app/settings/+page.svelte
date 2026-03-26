<script lang="ts">
	import { auth } from '$stores/auth';
	import { User, CreditCard } from 'lucide-svelte';
</script>

<svelte:head>
	<title>Settings - Heisenberg</title>
</svelte:head>

<div class="space-y-6">
	<div>
		<h1 class="text-3xl font-bold">Settings</h1>
		<p class="text-muted-foreground">Manage your account and preferences</p>
	</div>

	<div class="grid gap-4">
		<a
			href="/app/settings/billing"
			class="flex items-center gap-4 p-4 rounded-lg border border-border bg-card hover:bg-accent transition-colors"
		>
			<div class="p-2 rounded-md bg-primary/10">
				<CreditCard class="h-5 w-5 text-primary" />
			</div>
			<div>
				<h3 class="font-semibold">Billing</h3>
				<p class="text-sm text-muted-foreground">Manage your subscription and usage</p>
			</div>
		</a>

		<div class="p-4 rounded-lg border border-border bg-card">
			<div class="flex items-center gap-4">
				<div class="p-2 rounded-md bg-primary/10">
					<User class="h-5 w-5 text-primary" />
				</div>
				<div class="flex-1">
					<h3 class="font-semibold">Profile</h3>
					<p class="text-sm text-muted-foreground">{$auth.user?.email}</p>
					{#if $auth.user?.name}
						<p class="text-xs text-muted-foreground">{$auth.user?.name}</p>
					{/if}
				</div>
				<a
					href="/api/auth/login?prompt=login"
					class="px-4 py-2 text-sm font-medium border border-border rounded-md hover:bg-accent transition-colors"
				>
					Manage Account
				</a>
			</div>
		</div>
	</div>
</div>
