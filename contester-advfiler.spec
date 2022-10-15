%define debug_package %{nil}
%global goipath github.com/contester/advfiler
%global gomodulesmode GO111MODULE=auto
 
Name:		contester-advfiler
Version:	0.0.3
Release:	1%{?dist}
Summary:	Contester storage
 
License:	MIT
URL:		http://stingr.net
Source0:	%{name}-%{version}.tar.gz
 
BuildRequires:	golang
BuildRequires:  systemd
Requires(pre): shadow-utils
%{?systemd_requires}
 
%description
Contester storage server
 
%pre
getent group %{name} >/dev/null || groupadd -r %{name}
getent passwd %{name} >/dev/null || \
    useradd -r -g %{name} -d %{_sharedstatedir}/%{name} -s /sbin/nologin \
    -c "Contester storage" %{name}
exit 0
 
%post
%systemd_post %{name}.service %{name}.socket
 
%preun
%systemd_preun %{name}.service %{name}.socket
 
%postun
%systemd_postun_with_restart %{name}.service %{name}.socket
 
%prep
%setup -q
 
%build
%gobuild -o bin/contester-advfiler %{goipath}
%gobuild -o bin/contester-advfiler-backup %{goipath}/backup
 
%install
 
%{__install} -d $RPM_BUILD_ROOT%{_bindir}
%{__install} -v -D -t $RPM_BUILD_ROOT%{_bindir} bin/contester-advfiler bin/contester-advfiler-backup
%{__install} -d $RPM_BUILD_ROOT%{_unitdir}
%{__install} -v -D -t $RPM_BUILD_ROOT%{_unitdir} contester-advfiler.service
%{__install} -v -D -t $RPM_BUILD_ROOT%{_unitdir} contester-advfiler.socket
%{__install} -d -m 0755 %{buildroot}%{_sysconfdir}/%{name}
%{__install} -d $RPM_BUILD_ROOT%{_sysconfdir}/sysconfig
%{__install} -m 644 -T contester-advfiler.sysconfig %{buildroot}%{_sysconfdir}/sysconfig/contester-advfiler
%{__install} -d -m 0755 %{buildroot}%{_sharedstatedir}/%{name}
 
%files
%{_bindir}/contester-advfiler
%{_bindir}/contester-advfiler-backup
%dir %attr(-,%{name},%{name}) %{_sharedstatedir}/%{name}
%{_unitdir}/contester-advfiler.service
%{_unitdir}/contester-advfiler.socket
%config(noreplace) %{_sysconfdir}/%{name}
%config(noreplace) %{_sysconfdir}/sysconfig/contester-advfiler
 
%changelog
* Sun Oct 06 2019 Paul Komkoff <i@stingr.net> 0.0.3-1
- Remove key not found logspam (i@stingr.net)

* Sun Oct 06 2019 Paul Komkoff <i@stingr.net> 0.0.2-1
- new package built with tito

