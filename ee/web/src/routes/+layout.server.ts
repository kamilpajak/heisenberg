import { kindeAuthClient } from '@kinde-oss/kinde-auth-sveltekit';
import type { LayoutServerLoad } from './$types';

export const load: LayoutServerLoad = async ({ request }) => {
	const isAuthenticated = await kindeAuthClient.isAuthenticated(request);

	if (!isAuthenticated) {
		return {
			isAuthenticated: false,
			user: null,
			accessToken: null
		};
	}

	const user = await kindeAuthClient.getUser(request);
	const accessToken = await kindeAuthClient.getAccessToken(request);

	return {
		isAuthenticated: true,
		user,
		accessToken
	};
};
