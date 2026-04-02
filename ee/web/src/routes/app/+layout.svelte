<script lang="ts">
	import { auth } from '$stores/auth';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import { Home, Settings, LogOut } from 'lucide-svelte';

	$: if (!$auth.isLoading && !$auth.isAuthenticated) {
		goto('/');
	}

	$: currentPath = $page.url.pathname;

	const navItems = [
		{ href: '/app', label: 'Dashboard', icon: Home },
		{ href: '/app/settings', label: 'Settings', icon: Settings }
	];
</script>

{#if $auth.isLoading}
	<div class="min-h-screen flex items-center justify-center">
		<div class="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
	</div>
{:else if $auth.isAuthenticated}
	<div class="min-h-screen bg-background">
		<aside class="fixed inset-y-0 left-0 w-64 border-r border-border bg-card">
			<div class="p-4 border-b border-border">
				<span class="text-xl font-bold text-primary">Heisenberg</span>
			</div>

			<nav class="p-4 space-y-1">
				{#each navItems as item}
					<a
						href={item.href}
						class="flex items-center gap-3 px-3 py-2 rounded-md text-sm font-medium transition-colors {currentPath === item.href
							? 'bg-primary text-primary-foreground'
							: 'text-muted-foreground hover:bg-accent hover:text-foreground'}"
					>
						<svelte:component this={item.icon} class="h-4 w-4" />
						{item.label}
					</a>
				{/each}
			</nav>

			<div class="absolute bottom-0 left-0 right-0 p-4 border-t border-border">
				<div class="flex items-center gap-3 mb-4">
					<div class="h-8 w-8 rounded-full bg-primary/20 flex items-center justify-center">
						<span class="text-sm font-medium text-primary">
							{$auth.user?.email?.[0]?.toUpperCase() || 'U'}
						</span>
					</div>
					<div class="flex-1 min-w-0">
						<p class="text-sm font-medium truncate">{$auth.user?.name || $auth.user?.email}</p>
						{#if $auth.user?.name}
							<p class="text-xs text-muted-foreground truncate">{$auth.user?.email}</p>
						{/if}
					</div>
				</div>
				<a
					href="/api/auth/logout"
					class="w-full flex items-center gap-3 px-3 py-2 rounded-md text-sm font-medium text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
				>
					<LogOut class="h-4 w-4" />
					Sign Out
				</a>
			</div>
		</aside>

		<main class="ml-64 p-8">
			<slot />
		</main>
	</div>
{/if}
