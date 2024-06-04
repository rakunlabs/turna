const getRedirectPath = function () {
  const searchParams = (new URLSearchParams(window.location.search));
  // check redirect_path
  const redirectPath = searchParams.get('redirect_path');

  if (!!redirectPath) {
    return redirectPath;
  }

  return "/";
}

export { getRedirectPath };
