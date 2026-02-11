<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { auth } from '$stores/auth';
	import { syncUser, getMe } from '$lib/api';
	import type { LayoutData } from './$types';

	export let data: LayoutData;

	onMount(async () => {
		if (data.isAuthenticated && data.accessToken) {
			auth.setAccessToken(data.accessToken);

			try {
				// Sync user to our database
				await syncUser();
				const userData = await getMe();
				auth.setUser(userData);
			} catch (error) {
				console.error('Failed to sync user:', error);
				auth.setLoading(false);
			}
		} else {
			auth.setLoading(false);
		}
	});

	// Update auth state when data changes (e.g., after login)
	$: if (data.isAuthenticated && data.accessToken) {
		auth.setAccessToken(data.accessToken);
	}
</script>

<slot />
