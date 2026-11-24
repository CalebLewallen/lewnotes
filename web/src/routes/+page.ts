import { redirect } from '@sveltejs/kit';

import { api, ApiError, setCSRF, type Me } from '#lib/api';
import type { PageLoad } from './$types';

// The root picks the right first screen without flashing an app shell.
export const load: PageLoad = async ({ fetch }) => {
	const status = await api.authStatus(fetch);
	if (status.setup_required) redirect(307, '/setup');

	let me: Me;
	try {
		me = await api.me(fetch);
	} catch (error) {
		if (error instanceof ApiError && error.status === 401) redirect(307, '/login');
		throw error;
	}
	setCSRF(me.csrf_token);
	redirect(307, '/notes');
};
